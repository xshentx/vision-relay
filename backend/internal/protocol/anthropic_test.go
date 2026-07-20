package protocol

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamingTestWriter struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	flushed chan struct{}
}

func newStreamingTestWriter() *streamingTestWriter {
	return &streamingTestWriter{header: make(http.Header), flushed: make(chan struct{}, 16)}
}

func (w *streamingTestWriter) Header() http.Header {
	return w.header
}

func (w *streamingTestWriter) WriteHeader(int) {}

func (w *streamingTestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func (w *streamingTestWriter) Flush() {
	select {
	case w.flushed <- struct{}{}:
	default:
	}
}

func (w *streamingTestWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func TestWriteAnthropicStreamFlushesBeforeUpstreamCompletes(t *testing.T) {
	reader, writer := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       reader,
	}
	capture := newStreamingTestWriter()
	done := make(chan struct{})
	go func() {
		WriteAnthropicStreamFromChatCompletion(capture, resp)
		close(done)
	}()

	_, _ = io.WriteString(writer, "data: {\"id\":\"chatcmpl-live\",\"model\":\"live-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
	deadline := time.After(time.Second)
	for !strings.Contains(capture.String(), `"text":"hello"`) {
		select {
		case <-capture.flushed:
		case <-deadline:
			t.Fatal("first text delta was not flushed before the upstream stream completed")
		}
	}
	select {
	case <-done:
		t.Fatal("converter returned before the upstream stream completed")
	default:
	}

	_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n")
	_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	_ = writer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("converter did not finish after upstream completion")
	}
	body := capture.String()
	if !strings.Contains(body, `"input_tokens":3`) || !strings.Contains(body, `"output_tokens":1`) {
		t.Fatalf("final usage is missing:\n%s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("message_stop is missing:\n%s", body)
	}
}

func TestWriteAnthropicStreamConvertsToolDeltas(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl-tool","model":"tool-model","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Shanghai\"}"}}]},"finish_reason":"tool_calls"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":4}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteAnthropicStreamFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	for _, expected := range []string{
		`"type":"tool_use"`,
		`"name":"get_weather"`,
		`"partial_json":"{\"city\":"`,
		`"partial_json":"\"Shanghai\"}"`,
		`"stop_reason":"tool_use"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream is missing %q:\n%s", expected, body)
		}
	}
}

func TestWriteAnthropicStreamRejectsIncompleteUpstream(t *testing.T) {
	upstream := `data: {"id":"chatcmpl-cut-off","model":"test-model","choices":[{"delta":{"content":"partial"}}]}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteAnthropicStreamFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	if !strings.Contains(body, "event: error") || !strings.Contains(body, "upstream stream ended before completion") {
		t.Fatalf("incomplete stream did not produce an error:\n%s", body)
	}
	if strings.Contains(body, "event: message_stop") {
		t.Fatalf("incomplete stream was reported as successful:\n%s", body)
	}
}

func TestWriteAnthropicStreamConvertsJSONFallback(t *testing.T) {
	upstream := `{"id":"chatcmpl-json","model":"test-model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteAnthropicStreamFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	for _, expected := range []string{
		"event: message_start",
		`"text":"hello"`,
		`"input_tokens":5`,
		`"output_tokens":2`,
		"event: message_stop",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("synthetic stream is missing %q:\n%s", expected, body)
		}
	}
}
