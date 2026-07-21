package protocol

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteChatCompletionStreamFromSyncResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-sync","created":123,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
		}`)),
	}
	rec := httptest.NewRecorder()

	WriteChatCompletionStreamFromSyncResponse(rec, resp)

	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q, want event stream", got)
	}
	body := rec.Body.String()
	for _, expected := range []string{
		`"object":"chat.completion.chunk"`,
		`"content":"hello"`,
		`"name":"lookup"`,
		`"finish_reason":"tool_calls"`,
		`"total_tokens":5`,
		`data: [DONE]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("stream is missing %q:\n%s", expected, body)
		}
	}
}
