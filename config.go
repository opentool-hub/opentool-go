package opentool

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config controls the server listener and OpenTool HTTP routes.
type Config struct {
	Host    string
	Port    int
	Prefix  string
	Version string
	APIKeys []string
}

// DefaultConfig returns the standard OpenTool server configuration.
func DefaultConfig() Config {
	return Config{Host: DefaultHost, Port: DefaultPort, Prefix: DefaultPrefix, Version: DefaultToolTag}
}

func (c Config) validate() (Config, error) {
	if strings.TrimSpace(c.Host) == "" {
		return c, fmt.Errorf("server host must not be empty")
	}
	if c.Port < 0 || c.Port > 65535 {
		return c, fmt.Errorf("server port must be between 0 and 65535")
	}
	if c.Version == "" {
		c.Version = DefaultToolTag
	}
	prefix, err := normalizePrefix(c.Prefix)
	if err != nil {
		return c, err
	}
	c.Prefix = prefix
	c.APIKeys = normalizeAPIKeys(c.APIKeys)
	return c, nil
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return "", nil
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if strings.ContainsAny(prefix, "?#") {
		return "", fmt.Errorf("server prefix must be a URL path")
	}
	clean := strings.TrimRight(prefix, "/")
	if clean == "" {
		return "", nil
	}
	if strings.Contains(clean, "//") {
		return "", fmt.Errorf("server prefix must not contain empty path segments")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		if segment == "." || segment == ".." || strings.Contains(segment, "%") {
			return "", fmt.Errorf("server prefix contains an invalid path segment")
		}
		if _, err := url.PathUnescape(segment); err != nil {
			return "", fmt.Errorf("server prefix contains invalid escaping: %w", err)
		}
	}
	return clean, nil
}

func normalizeAPIKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, group := range keys {
		for _, key := range strings.Split(group, ",") {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, key)
		}
	}
	return result
}

type serverOptions struct {
	config       Config
	spec         []byte
	specSet      bool
	maxBodyBytes int64
	logger       *slog.Logger
	cli          *CLIArguments
}

// Option configures a server at construction time.
type Option func(*serverOptions) error

// WithConfig replaces the default server configuration.
func WithConfig(config Config) Option {
	return func(options *serverOptions) error {
		options.config = config
		return nil
	}
}

// WithHost sets the listener host.
func WithHost(host string) Option {
	return func(options *serverOptions) error {
		options.config.Host = host
		return nil
	}
}

// WithPort sets the listener port. Zero requests an operating-system-selected
// port when serving on a caller-provided listener.
func WithPort(port int) Option {
	return func(options *serverOptions) error {
		options.config.Port = port
		return nil
	}
}

// WithPrefix sets the route prefix. An empty prefix mounts endpoints at root.
func WithPrefix(prefix string) Option {
	return func(options *serverOptions) error {
		options.config.Prefix = prefix
		return nil
	}
}

// WithVersion sets the value returned by GET /version.
func WithVersion(version string) Option {
	return func(options *serverOptions) error {
		options.config.Version = version
		return nil
	}
}

// WithAPIKeys replaces accepted Bearer API keys. An empty list disables auth.
func WithAPIKeys(keys ...string) Option {
	return func(options *serverOptions) error {
		options.config.APIKeys = append([]string(nil), keys...)
		return nil
	}
}

// WithSpec configures a raw OpenTool JSON description.
func WithSpec(raw []byte) Option {
	return func(options *serverOptions) error {
		options.spec = append([]byte(nil), raw...)
		options.specSet = true
		return nil
	}
}

// WithSpecFile loads an OpenTool JSON description from a file.
func WithSpecFile(path string) Option {
	return func(options *serverOptions) error {
		spec, err := LoadSpecFile(path)
		if err != nil {
			return err
		}
		options.spec = spec.RawJSON()
		options.specSet = true
		return nil
	}
}

// WithMaxRequestBodyBytes sets the maximum body size for call endpoints.
func WithMaxRequestBodyBytes(limit int64) Option {
	return func(options *serverOptions) error {
		if limit <= 0 {
			return fmt.Errorf("maximum request body size must be positive")
		}
		options.maxBodyBytes = limit
		return nil
	}
}

// WithLogger selects the logger used for request and lifecycle failures.
func WithLogger(logger *slog.Logger) Option {
	return func(options *serverOptions) error {
		if logger == nil {
			return fmt.Errorf("logger must not be nil")
		}
		options.logger = logger
		return nil
	}
}

// WithCLIArguments opts into parsing the supplied arguments during Init.
func WithCLIArguments(arguments CLIArguments) Option {
	return func(options *serverOptions) error {
		copy := arguments.clone()
		options.cli = &copy
		return nil
	}
}

// WithProcessCLIArguments explicitly opts into parsing os.Args.
func WithProcessCLIArguments() Option {
	return WithCLIArguments(NewCLIArguments(os.Args[1:]...))
}

func hostPort(config Config) string {
	return net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
}
