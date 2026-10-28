package opentool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultIdleTimeout       = 2 * time.Minute
	defaultShutdownTimeout   = 30 * time.Second
)

// Server hosts one Tool over the OpenTool HTTP protocol.
type Server struct {
	tool     Tool
	configMu sync.RWMutex
	config   Config
	spec     *Spec
	maxBody  int64
	logger   *slog.Logger
	cli      *CLIArguments

	initMu   sync.Mutex
	initDone bool
	initErr  error

	stateMu      sync.Mutex
	shuttingDown bool
	requests     sync.WaitGroup
	streamCancel map[uint64]context.CancelFunc
	nextStream   uint64

	httpMu     sync.Mutex
	httpServer *http.Server

	shutdownOnce sync.Once
	disposeOnce  sync.Once
	done         chan struct{}
	shutdownErr  error
	disposeErr   error
}

// NewServer creates a server with OpenTool defaults and the supplied options.
func NewServer(tool Tool, options ...Option) (*Server, error) {
	if tool == nil {
		return nil, fmt.Errorf("tool must not be nil")
	}
	configured := serverOptions{
		config:       DefaultConfig(),
		maxBodyBytes: DefaultMaxRequestBodyBytes,
		logger:       slog.Default(),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
	}
	config, err := configured.config.validate()
	if err != nil {
		return nil, err
	}
	var spec *Spec
	if configured.specSet {
		spec, err = ParseSpec(configured.spec)
		if err != nil {
			return nil, err
		}
	}
	server := &Server{
		tool:         tool,
		config:       config,
		spec:         spec,
		maxBody:      configured.maxBodyBytes,
		logger:       configured.logger,
		cli:          configured.cli,
		streamCancel: make(map[uint64]context.CancelFunc),
		done:         make(chan struct{}),
	}
	return server, nil
}

// Config returns a copy of the active configuration.
func (s *Server) Config() Config {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	config := s.config
	config.APIKeys = append([]string(nil), s.config.APIKeys...)
	return config
}

// Handler returns an HTTP handler for mounting in another Go HTTP server.
// Call Init before serving requests when the tool uses lifecycle hooks.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.ServeHTTP) }

// Done closes after shutdown and resource disposal complete.
func (s *Server) Done() <-chan struct{} { return s.done }

// Init resolves optional CLI arguments and initializes the tool once.
func (s *Server) Init(ctx context.Context) error {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.initDone {
		return s.initErr
	}
	s.stateMu.Lock()
	stopping := s.shuttingDown
	s.stateMu.Unlock()
	if stopping {
		return fmt.Errorf("server is shutting down")
	}
	s.initDone = true
	args := ParsedArgs{Custom: make(map[string]CLIValue)}
	if s.cli != nil {
		parsed, err := s.cli.Parse()
		if err != nil {
			s.initErr = err
			return err
		}
		args = parsed
	}
	if initializer, ok := s.tool.(ArgsInitializer); ok {
		if err := initializer.InitArgs(ctx, &args); err != nil {
			s.initErr = fmt.Errorf("initialize tool arguments: %w", err)
			return s.initErr
		}
	}
	s.configMu.Lock()
	if args.Tag != "" {
		s.config.Version = args.Tag
	}
	if args.Host != "" {
		s.config.Host = args.Host
	}
	if args.Port != 0 {
		s.config.Port = args.Port
	}
	if len(args.APIKeys) > 0 {
		s.config.APIKeys = args.APIKeys
	}
	config, err := s.config.validate()
	if err == nil {
		s.config = config
	}
	s.configMu.Unlock()
	if err != nil {
		s.initErr = err
		return err
	}
	if initializer, ok := s.tool.(Initializer); ok {
		if err := initializer.Init(ctx); err != nil {
			s.initErr = fmt.Errorf("initialize tool: %w", err)
			disposeCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
			defer cancel()
			s.disposeOnce.Do(func() { s.disposeErr = s.dispose(disposeCtx) })
			s.initErr = errors.Join(s.initErr, s.disposeErr)
			return s.initErr
		}
	}
	return nil
}

// Serve initializes the tool and serves on a caller-provided listener.
func (s *Server) Serve(listener net.Listener) error {
	if err := s.Init(context.Background()); err != nil {
		return err
	}
	return s.serveInitialized(listener)
}

// ListenAndServe initializes the tool, binds the configured address and serves.
func (s *Server) ListenAndServe() error {
	if err := s.Init(context.Background()); err != nil {
		return err
	}
	config := s.Config()
	listener, err := net.Listen("tcp", hostPort(config))
	if err != nil {
		shutdownErr := s.Shutdown(context.Background())
		return errors.Join(fmt.Errorf("listen on %s: %w", hostPort(config), err), shutdownErr)
	}
	return s.serveInitialized(listener)
}

func (s *Server) serveInitialized(listener net.Listener) error {
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
	s.stateMu.Lock()
	if s.shuttingDown {
		s.stateMu.Unlock()
		_ = listener.Close()
		return fmt.Errorf("server is shutting down")
	}
	s.httpMu.Lock()
	if s.httpServer != nil {
		s.httpMu.Unlock()
		s.stateMu.Unlock()
		_ = listener.Close()
		return fmt.Errorf("server is already serving")
	}
	s.httpServer = server
	s.httpMu.Unlock()
	s.stateMu.Unlock()

	err := server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("OpenTool HTTP server failed", "error", err)
	}
	shutdownErr := s.Shutdown(context.Background())
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
}

// Shutdown stops accepting work, cancels active streams, waits for requests,
// and calls Dispose once. The operation continues in the background if ctx
// expires; callers may wait again using Done.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.shutdownOnce.Do(func() { go s.performShutdown() })
	select {
	case <-s.done:
		return s.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop is an alias for Shutdown.
func (s *Server) Stop(ctx context.Context) error { return s.Shutdown(ctx) }

func (s *Server) performShutdown() {
	s.stateMu.Lock()
	s.shuttingDown = true
	cancels := make([]context.CancelFunc, 0, len(s.streamCancel))
	for _, cancel := range s.streamCancel {
		cancels = append(cancels, cancel)
	}
	s.stateMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}

	s.httpMu.Lock()
	httpServer := s.httpServer
	s.httpMu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			s.shutdownErr = errors.Join(s.shutdownErr, fmt.Errorf("shut down HTTP server: %w", err))
			if closeErr := httpServer.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				s.shutdownErr = errors.Join(s.shutdownErr, closeErr)
			}
		}
	}
	requestsDone := make(chan struct{})
	go func() {
		s.requests.Wait()
		close(requestsDone)
	}()
	select {
	case <-requestsDone:
	case <-shutdownCtx.Done():
		s.shutdownErr = errors.Join(s.shutdownErr, fmt.Errorf("wait for requests: %w", shutdownCtx.Err()))
		if httpServer != nil {
			_ = httpServer.Close()
		}
	}
	cancel()

	s.initMu.Lock()
	disposeCtx, disposeCancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	s.disposeOnce.Do(func() { s.disposeErr = s.dispose(disposeCtx) })
	s.initMu.Unlock()
	disposeCancel()
	if s.disposeErr != nil {
		s.shutdownErr = errors.Join(s.shutdownErr, s.disposeErr)
	}
	close(s.done)
}

func (s *Server) dispose(ctx context.Context) error {
	if disposer, ok := s.tool.(Disposer); ok {
		if err := disposer.Dispose(ctx); err != nil {
			return fmt.Errorf("dispose tool: %w", err)
		}
	}
	return nil
}

func (s *Server) beginRequest() (func(), bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.shuttingDown {
		return nil, false
	}
	s.requests.Add(1)
	return s.requests.Done, true
}

func (s *Server) addStream(cancel context.CancelFunc) (uint64, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.shuttingDown {
		return 0, false
	}
	s.nextStream++
	s.streamCancel[s.nextStream] = cancel
	return s.nextStream, true
}

func (s *Server) removeStream(id uint64) {
	s.stateMu.Lock()
	delete(s.streamCancel, id)
	s.stateMu.Unlock()
}
