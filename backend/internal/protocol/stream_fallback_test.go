package protocol

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteGeminiStreamFromSyncResponseAsSSE(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`)),
	}
	rec := httptest.NewRecorder()

	WriteGeminiStreamFromSyncResponse(rec, resp, true)

	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q, want event stream", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `data: {"candidates"`) || !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("unexpected Gemini SSE: %s", body)
	}
}

func TestWriteGeminiStreamFromSyncResponseAsJSONArray(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[]}`)),
	}
	rec := httptest.NewRecorder()

	WriteGeminiStreamFromSyncResponse(rec, resp, false)

	if got, want := strings.TrimSpace(rec.Body.String()), `[{"candidates":[]}]`; got != want {
		t.Fatalf("Gemini JSON stream = %q, want %q", got, want)
	}
}

func TestWriteOllamaStreamFromSyncResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"model":"test","message":{"role":"assistant","content":"hello"},"done":true}`)),
	}
	rec := httptest.NewRecorder()

	WriteOllamaStreamFromSyncResponse(rec, resp)

	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/x-ndjson") {
		t.Fatalf("content type = %q, want NDJSON", got)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(body, "\n") || !strings.Contains(body, `"done":true`) {
		t.Fatalf("unexpected Ollama stream: %q", body)
	}
}
