package opentool

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

type trackedWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *trackedWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackedWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *trackedWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *trackedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ServeHTTP implements http.Handler. Handler is the preferred method when
// mounting the SDK into a larger HTTP server.
func (s *Server) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	w := &trackedWriter{ResponseWriter: responseWriter}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("panic while handling OpenTool request", "panic", recovered)
			if !w.wrote {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}
	}()

	config := s.Config()
	path := request.URL.Path
	base := config.Prefix
	if base != "" && path != base && !strings.HasPrefix(path, base+"/") {
		http.NotFound(w, request)
		return
	}
	endpoint := strings.TrimPrefix(path, base)
	if endpoint == "" {
		http.NotFound(w, request)
		return
	}
	known := endpoint == "/version" || endpoint == "/load" || endpoint == "/call" || endpoint == "/streamCall" || endpoint == "/stop"
	if !known {
		http.NotFound(w, request)
		return
	}
	allowedMethod := map[string]string{
		"/version":    http.MethodGet,
		"/load":       http.MethodGet,
		"/call":       http.MethodPost,
		"/streamCall": http.MethodPost,
		"/stop":       http.MethodPost,
	}[endpoint]
	if request.Method != allowedMethod {
		w.Header().Set("Allow", allowedMethod)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !authorize(request, config.APIKeys) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing bearer token"})
		return
	}
	release, ok := s.beginRequest()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server is shutting down"})
		return
	}
	defer release()

	switch endpoint {
	case "/version":
		writeJSON(w, http.StatusOK, map[string]string{"version": config.Version})
	case "/load":
		s.handleLoad(w, request)
	case "/call":
		s.handleCall(w, request)
	case "/streamCall":
		s.handleStreamCall(w, request)
	case "/stop":
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
		go func() { _ = s.Shutdown(context.Background()) }()
	}
}

func authorize(request *http.Request, keys []string) bool {
	if len(keys) == 0 {
		return true
	}
	header := request.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return false
	}
	matched := 0
	for _, key := range keys {
		matched |= subtle.ConstantTimeCompare([]byte(token), []byte(key))
	}
	return matched == 1
}

func (s *Server) handleCall(w *trackedWriter, r *http.Request) {
	request, err := s.readRequest(w, r)
	if err != nil {
		s.writeRequestError(w, err)
		return
	}
	result, err := invokeCall(s.tool, r.Context(), request.Method, request.Params)
	if err != nil {
		writeJSON(w, http.StatusOK, errorResponse(asRPCError(err), request.ID))
		return
	}
	writeJSON(w, http.StatusOK, successResponse(result, request.ID))
}

func (s *Server) handleLoad(w *trackedWriter, r *http.Request) {
	if s.spec != nil {
		writeRawJSON(w, http.StatusOK, s.spec.RawJSON())
		return
	}
	provider, ok := s.tool.(SpecProvider)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"code": CodeNotConfigured, "message": "Json Parser NOT implement"})
		return
	}
	raw, err := invokeLoad(provider, r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	spec, err := ParseSpec(raw)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeRawJSON(w, http.StatusOK, spec.RawJSON())
}

func (s *Server) handleStreamCall(w *trackedWriter, r *http.Request) {
	request, err := s.readRequest(w, r)
	if err != nil {
		s.writeRequestError(w, err)
		return
	}
	streamTool, ok := s.tool.(StreamTool)
	if !ok {
		writeJSON(w, http.StatusOK, errorResponse(asRPCError(StreamNotSupported()), request.ID))
		return
	}
	streamCtx, cancel := context.WithCancel(r.Context())
	streamID, accepted := s.addStream(cancel)
	if !accepted {
		cancel()
		writeJSON(w, http.StatusServiceUnavailable, errorResponse(&RPCError{Code: CodeUnavailable, Message: "server is shutting down"}, request.ID))
		return
	}
	defer func() {
		s.removeStream(streamID)
		cancel()
	}()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	var writeMu sync.Mutex
	if err := writeSSE(w, EventStart, map[string]string{EventStart: request.Method}); err != nil {
		return
	}
	active := true
	emit := func(result any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if !active {
			return fmt.Errorf("streamCall has already returned")
		}
		if err := streamCtx.Err(); err != nil {
			return err
		}
		return writeSSE(w, EventData, successResponse(result, request.ID))
	}
	if err := invokeStreamCall(streamTool, streamCtx, request.Method, request.Params, emit); err != nil {
		writeMu.Lock()
		active = false
		defer writeMu.Unlock()
		if streamCtx.Err() != nil {
			return
		}
		_ = writeSSE(w, EventError, errorResponse(asRPCError(err), request.ID))
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	active = false
	if streamCtx.Err() == nil {
		_ = writeSSE(w, EventDone, map[string]string{EventDone: request.Method})
	}
}

func invokeCall(tool Tool, ctx context.Context, method string, params json.RawMessage) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool panicked: %v", recovered)
		}
	}()
	return tool.Call(ctx, method, params)
}

func invokeLoad(tool SpecProvider, ctx context.Context) (spec json.RawMessage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool panicked: %v", recovered)
		}
	}()
	return tool.Load(ctx)
}

func invokeStreamCall(tool StreamTool, ctx context.Context, method string, params json.RawMessage, emit func(any) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool panicked: %v", recovered)
		}
	}()
	return tool.StreamCall(ctx, method, params, emit)
}

func (s *Server) readRequest(w *trackedWriter, r *http.Request) (Request, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Request{}, &httpInputError{status: http.StatusBadRequest, body: map[string]string{"error": "Content-Type must be application/json"}}
	}
	reader := http.MaxBytesReader(w, r.Body, s.maxBody)
	data, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return Request{}, &httpInputError{status: http.StatusRequestEntityTooLarge, body: map[string]string{"error": "request body too large"}}
		}
		return Request{}, &httpInputError{status: http.StatusBadRequest, body: map[string]string{"error": "failed to read request body"}}
	}
	request, err := ParseRequest(data)
	if err != nil {
		return Request{}, err
	}
	return request, nil
}

type httpInputError struct {
	status int
	body   any
}

func (e *httpInputError) Error() string { return fmt.Sprintf("HTTP input error %d", e.status) }

func (s *Server) writeRequestError(w *trackedWriter, err error) {
	var inputErr *httpInputError
	if errors.As(err, &inputErr) {
		writeJSON(w, inputErr.status, inputErr.body)
		return
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		writeJSON(w, http.StatusBadRequest, errorResponse(&RPCError{Code: requestErr.Code, Message: requestErr.Message}, requestErr.ID))
		return
	}
	writeJSON(w, http.StatusBadRequest, errorResponse(&RPCError{Code: CodeInvalidRequest, Message: err.Error()}, nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"failed to encode response"}`)
		return
	}
	writeRawJSON(w, status, data)
}

func writeRawJSON(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
