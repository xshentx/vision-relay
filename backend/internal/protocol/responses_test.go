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

func TestWriteStreamingResponsesFromSyncChatCompletion(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-sync","model":"test-model","created":123,
			"choices":[{"message":{"role":"assistant","content":"sync fallback"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)),
	}
	recorder := httptest.NewRecorder()

	WriteStreamingResponsesFromSyncChatCompletion(recorder, resp)

	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q, want event stream", got)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`"type":"response.created"`,
		`"type":"response.output_text.delta"`,
		`"delta":"sync fallback"`,
		`"type":"response.completed"`,
		`data: [DONE]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("sync fallback stream is missing %q:\n%s", expected, body)
		}
	}
}

func TestWriteStreamingResponsesFromSyncResponsePreservesFunctionCall(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp-sync","object":"response","status":"completed","model":"test-model",
			"output":[{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}]
		}`)),
	}
	recorder := httptest.NewRecorder()

	WriteStreamingResponsesFromSyncResponse(recorder, resp)

	body := recorder.Body.String()
	for _, expected := range []string{
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"type":"response.completed"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("function-call fallback stream is missing %q:\n%s", expected, body)
		}
	}
}
