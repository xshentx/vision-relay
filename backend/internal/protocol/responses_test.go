package protocol

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentToTextIgnoresNull(t *testing.T) {
	if got := contentToText(nil); got != "" {
		t.Fatalf("contentToText(nil) = %q, want empty string", got)
	}
}

func TestWriteStreamingResponsesSkipsNullContentDeltas(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"chatcmpl-1","model":"test-model","choices":[{"delta":{"role":"assistant","content":null}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":null}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"hello"}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
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

	WriteStreamingResponsesFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	if strings.Contains(body, `"delta":"null"`) {
		t.Fatalf("stream contains a null text delta:\n%s", body)
	}
	if got := strings.Count(body, `"type":"response.output_text.delta"`); got != 1 {
		t.Fatalf("text delta event count = %d, want 1:\n%s", got, body)
	}
	if !strings.Contains(body, `"delta":"hello"`) {
		t.Fatalf("stream is missing the real text delta:\n%s", body)
	}
}
