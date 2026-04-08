package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// TestCompleteStream_IncrementalDelivery guards against the 757e4c3-class
// regression where chunks arrive all at once instead of token-by-token.
//
// Failure mode it catches:
//   - Transport.ForceAttemptHTTP2 = true (HTTP/2 flow-control holds SSE frames).
//   - Transport.DisableCompression = false + gzipped response (gzip.Reader
//     buffers the whole DEFLATE window before emitting any bytes).
//   - Any future change that introduces a buffered reader between the socket
//     and the SSE scanner.
//
// How it detects the failure:
//
//	The mock server writes 5 SSE chunks with 40ms gaps, flushing after each.
//	A healthy client delivers onChunk calls with inter-call gaps ≥ ~30ms and
//	the first chunk arrives well before the last write. A buffered client
//	delivers all chunks in a burst at EOF, making inter-chunk gaps near zero
//	and the first delivery time roughly equal to the total stream duration.
func TestCompleteStream_IncrementalDelivery(t *testing.T) {
	const (
		chunkCount = 5
		writeGap   = 40 * time.Millisecond
	)

	var gotAcceptEncoding string
	srv, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}

		for i := range chunkCount {
			payload := fmt.Sprintf(
				`data: {"choices":[{"delta":{"content":"tok%d"},"finish_reason":null}]}`+"\n\n",
				i,
			)
			if _, err := w.Write([]byte(payload)); err != nil {
				return
			}
			flusher.Flush()
			time.Sleep(writeGap)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})
	_ = srv

	var (
		mu         sync.Mutex
		chunkTimes []time.Time
		contents   []string
	)
	onChunk := func(chunk string) {
		mu.Lock()
		chunkTimes = append(chunkTimes, time.Now())
		contents = append(contents, chunk)
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.CompleteStream(ctx, &cpn.LLMRequest{
		Model:    "test-default-model",
		Messages: []*cpn.LLMMessage{{Role: "user", Content: "hi"}},
	}, onChunk)
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}

	// ── Assertions ──────────────────────────────────────────────────────────

	// 1. Accept-Encoding must be identity so upstream CDNs never gzip
	//    the stream (gzip.Reader buffers DEFLATE windows and breaks
	//    incremental delivery).
	if gotAcceptEncoding != "identity" {
		t.Errorf("Accept-Encoding = %q, want %q (streaming MUST NOT be compressed)",
			gotAcceptEncoding, "identity")
	}

	// 2. Every chunk must have been delivered to onChunk.
	if len(chunkTimes) != chunkCount {
		t.Fatalf("onChunk called %d times, want %d", len(chunkTimes), chunkCount)
	}

	// 3. Full response content must reassemble correctly.
	if got := resp.Content; got != "tok0tok1tok2tok3tok4" {
		t.Errorf("resp.Content = %q, want concatenation of all 5 tokens", got)
	}

	// 4. First chunk must arrive well before the total stream duration.
	//    If it arrives only at end-of-stream, the transport is buffering.
	//    Threshold: first chunk within ~2× writeGap of request start.
	firstDelay := chunkTimes[0].Sub(start)
	totalDuration := time.Since(start)
	maxFirstDelay := 3 * writeGap // generous for CI jitter
	if firstDelay > maxFirstDelay {
		t.Errorf("first chunk delivered after %v (total stream took %v); "+
			"want first chunk within %v — transport is buffering",
			firstDelay, totalDuration, maxFirstDelay)
	}

	// 5. Inter-chunk gaps must be non-trivial. A buffered client delivers
	//    all chunks in a sub-millisecond burst; a healthy client preserves
	//    the server's 40ms pacing. We require at least 3 of 4 gaps to be
	//    ≥ 15ms (half the write gap, with margin for scheduling noise).
	const minGap = 15 * time.Millisecond
	nontrivialGaps := 0
	for i := 1; i < len(chunkTimes); i++ {
		if chunkTimes[i].Sub(chunkTimes[i-1]) >= minGap {
			nontrivialGaps++
		}
	}
	if nontrivialGaps < 3 {
		t.Errorf("only %d of %d inter-chunk gaps were ≥ %v; "+
			"chunks arrived in a burst — transport is buffering",
			nontrivialGaps, len(chunkTimes)-1, minGap)
	}
}
