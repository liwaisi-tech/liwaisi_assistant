package httpapi

import (
	"fmt"
	"io"
)

// WriteSSEEvent writes a single SSE event to the writer.
// Format: "id: {id}\nevent: {eventType}\ndata: {data}\n\n"
// If id is empty, the id field is omitted.
// If eventType is empty, the event field is omitted.
func WriteSSEEvent(w io.Writer, id, eventType string, data []byte) error {
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if eventType != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

// WriteSSEComment writes an SSE comment line.
// Format: ": {comment}\n\n"
// Used for heartbeat/keepalive signals.
func WriteSSEComment(w io.Writer, comment string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", comment)
	return err
}

// WriteSSERetry writes an SSE retry directive.
// Format: "retry: {ms}\n\n"
func WriteSSERetry(w io.Writer, milliseconds int) error {
	_, err := fmt.Fprintf(w, "retry: %d\n\n", milliseconds)
	return err
}
