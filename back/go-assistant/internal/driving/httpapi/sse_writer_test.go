package httpapi

import (
	"bytes"
	"testing"
)

func TestWriteSSEEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		id        string
		eventType string
		data      []byte
		want      string
	}{
		{
			name:      "full event with id, type, and data",
			id:        "42",
			eventType: "stream_chunk",
			data:      []byte(`{"content":"hello"}`),
			want:      "id: 42\nevent: stream_chunk\ndata: {\"content\":\"hello\"}\n\n",
		},
		{
			name:      "no id",
			id:        "",
			eventType: "transition_fired",
			data:      []byte(`{"tid":"t1"}`),
			want:      "event: transition_fired\ndata: {\"tid\":\"t1\"}\n\n",
		},
		{
			name:      "no event type",
			id:        "7",
			eventType: "",
			data:      []byte(`{"msg":"ok"}`),
			want:      "id: 7\ndata: {\"msg\":\"ok\"}\n\n",
		},
		{
			name:      "minimal data only",
			id:        "",
			eventType: "",
			data:      []byte("plain text"),
			want:      "data: plain text\n\n",
		},
		{
			name:      "empty data",
			id:        "1",
			eventType: "ping",
			data:      []byte(""),
			want:      "id: 1\nevent: ping\ndata: \n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := WriteSSEEvent(&buf, tt.id, tt.eventType, tt.data)
			if err != nil {
				t.Fatalf("WriteSSEEvent() error = %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("WriteSSEEvent() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestWriteSSEComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		comment string
		want    string
	}{
		{
			name:    "heartbeat",
			comment: "heartbeat",
			want:    ": heartbeat\n\n",
		},
		{
			name:    "keepalive",
			comment: "keepalive",
			want:    ": keepalive\n\n",
		},
		{
			name:    "empty comment",
			comment: "",
			want:    ": \n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := WriteSSEComment(&buf, tt.comment)
			if err != nil {
				t.Fatalf("WriteSSEComment() error = %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("WriteSSEComment() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteSSERetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		milliseconds int
		want         string
	}{
		{
			name:         "standard retry",
			milliseconds: 3000,
			want:         "retry: 3000\n\n",
		},
		{
			name:         "zero retry",
			milliseconds: 0,
			want:         "retry: 0\n\n",
		},
		{
			name:         "large retry",
			milliseconds: 60000,
			want:         "retry: 60000\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := WriteSSERetry(&buf, tt.milliseconds)
			if err != nil {
				t.Fatalf("WriteSSERetry() error = %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("WriteSSERetry() = %q, want %q", got, tt.want)
			}
		})
	}
}
