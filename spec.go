package opentool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Spec holds the OpenTool JSON value. Unknown fields and $ref strings are
// preserved when the value is returned from /load.
type Spec struct{ raw json.RawMessage }

// ParseSpec checks that data is one JSON object and retains its original bytes.
func ParseSpec(data []byte) (*Spec, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid OpenTool JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("invalid OpenTool JSON: trailing JSON value")
		}
		return nil, fmt.Errorf("invalid OpenTool JSON: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("OpenTool specification must be a JSON object")
	}
	return &Spec{raw: append(json.RawMessage(nil), raw...)}, nil
}

// LoadSpecFile loads and validates a specification from a file.
func LoadSpecFile(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OpenTool specification: %w", err)
	}
	return ParseSpec(data)
}

// RawJSON returns a copy of the retained JSON description.
func (s *Spec) RawJSON() json.RawMessage {
	if s == nil {
		return nil
	}
	return append(json.RawMessage(nil), s.raw...)
}
