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

func TestWriteStreamingResponsesRejectsTruncatedUpstream(t *testing.T) {
	upstream := "data: {\"id\":\"chatcmpl-1\",\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteStreamingResponsesFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"response.failed"`) {
		t.Fatalf("truncated stream is missing response.failed:\n%s", body)
	}
	if strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("truncated stream was marked completed:\n%s", body)
	}
}

func TestWriteStreamingResponsesAcceptsFinishReasonWithoutDone(t *testing.T) {
	upstream := "data: {\"id\":\"chatcmpl-1\",\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteStreamingResponsesFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"response.completed"`) || strings.Contains(body, `"type":"response.failed"`) {
		t.Fatalf("finish_reason stream did not complete normally:\n%s", body)
	}
}

func TestWriteStreamingResponsesReportsScannerErrors(t *testing.T) {
	upstream := "data: " + strings.Repeat("x", maxStreamEventSize+1)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}
	recorder := httptest.NewRecorder()

	WriteStreamingResponsesFromChatCompletion(recorder, resp)

	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"response.failed"`) || strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("scanner error did not fail the stream:\n%s", body)
	}
}
