package opentool

import (
	"fmt"
	"strconv"
	"strings"
)

// CLIValue stores values for a tool-specific command-line option.
type CLIValue struct {
	Values []string
	Multi  bool
}

// ParsedArgs contains SDK settings and tool-specific command-line arguments.
type ParsedArgs struct {
	Tag         string
	Host        string
	Port        int
	APIKeys     []string
	Custom      map[string]CLIValue
	Passthrough []string
}

// String returns a custom argument only when it has exactly one value.
func (a *ParsedArgs) String(name string) (string, bool) {
	value, ok := a.Custom[name]
	if !ok || len(value.Values) != 1 {
		return "", false
	}
	return value.Values[0], true
}

// Strings returns all values associated with a custom argument.
func (a *ParsedArgs) Strings(name string) ([]string, bool) {
	value, ok := a.Custom[name]
	if !ok {
		return nil, false
	}
	return append([]string(nil), value.Values...), true
}

type customOption struct{ multi, splitCommas bool }

// CLIArguments parses standard OpenTool options and preserves tool-owned flags.
type CLIArguments struct {
	args    []string
	options map[string]customOption
}

// NewCLIArguments creates a parser from arguments without the executable name.
func NewCLIArguments(args ...string) CLIArguments {
	return CLIArguments{args: append([]string(nil), args...), options: make(map[string]customOption)}
}

// AddCustomOption registers a single-value tool argument.
func (a *CLIArguments) AddCustomOption(name string) *CLIArguments {
	if a.options == nil {
		a.options = make(map[string]customOption)
	}
	a.options[name] = customOption{}
	return a
}

// AddCustomMultiOption registers a repeated, comma-splitting tool argument.
func (a *CLIArguments) AddCustomMultiOption(name string) *CLIArguments {
	if a.options == nil {
		a.options = make(map[string]customOption)
	}
	a.options[name] = customOption{multi: true, splitCommas: true}
	return a
}

func (a CLIArguments) clone() CLIArguments {
	copy := NewCLIArguments(a.args...)
	for name, option := range a.options {
		copy.options[name] = option
	}
	return copy
}

// Parse reads options in --name value and --name=value forms. Unknown options
// are retained as single-value custom arguments.
func (a CLIArguments) Parse() (ParsedArgs, error) {
	parsed := ParsedArgs{Custom: make(map[string]CLIValue)}
	for i := 0; i < len(a.args); i++ {
		token := a.args[i]
		if token == "--" {
			parsed.Passthrough = append(parsed.Passthrough, a.args[i+1:]...)
			break
		}
		if !strings.HasPrefix(token, "--") {
			parsed.Passthrough = append(parsed.Passthrough, token)
			continue
		}
		name, value, inline := strings.Cut(strings.TrimPrefix(token, "--"), "=")
		config, registered := a.options[name]
		standard := name == CLIArgumentTag || name == CLIArgumentHost || name == CLIArgumentPort || name == CLIArgumentAPIKeys
		if !inline && i+1 < len(a.args) && !strings.HasPrefix(a.args[i+1], "--") {
			value = a.args[i+1]
			i++
			inline = true
		}
		if !inline && (standard || registered) {
			return ParsedArgs{}, fmt.Errorf("missing value for --%s", name)
		}
		if !inline {
			value = "true"
		}
		switch name {
		case CLIArgumentTag:
			parsed.Tag = value
		case CLIArgumentHost:
			parsed.Host = value
		case CLIArgumentPort:
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return ParsedArgs{}, fmt.Errorf("invalid port for --%s: %q", name, value)
			}
			parsed.Port = port
		case CLIArgumentAPIKeys:
			parsed.APIKeys = append(parsed.APIKeys, splitValues(value)...)
		default:
			if !registered {
				config = customOption{}
			}
			values := []string{value}
			if config.splitCommas {
				values = splitValues(value)
			}
			if config.multi {
				existing := parsed.Custom[name]
				existing.Multi = true
				existing.Values = append(existing.Values, values...)
				parsed.Custom[name] = existing
			} else if len(values) > 0 {
				parsed.Custom[name] = CLIValue{Values: values}
			}
		}
	}
	return parsed, nil
}

func splitValues(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
