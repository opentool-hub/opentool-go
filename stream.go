package opentool

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func writeSSE(w http.ResponseWriter, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode SSE event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event:%s\ndata:%s\n\n", event, data); err != nil {
		return fmt.Errorf("write SSE event: %w", err)
	}
	flusher, ok := w.(http.Flusher)
	if tracked, trackedOK := w.(*trackedWriter); trackedOK {
		flusher, ok = tracked.ResponseWriter.(http.Flusher)
	}
	if !ok {
		return fmt.Errorf("HTTP response writer does not support streaming")
	}
	if tracked, trackedOK := w.(*trackedWriter); trackedOK {
		tracked.Flush()
	} else {
		flusher.Flush()
	}
	return nil
}
