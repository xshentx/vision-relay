package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIResponsesRetriesSynchronouslyWhenStreamingIsUnsupported(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		stream, _ := payload["stream"].(bool)
		if calls == 1 {
			if !stream {
				t.Fatalf("first request should prefer streaming: %#v", payload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "stream unsupported"}})
			return
		}
		if stream {
			t.Fatalf("fallback request should be synchronous: %#v", payload)
		}
		if _, ok := payload["stream_options"]; ok {
			t.Fatalf("sync fallback retained stream_options: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-fallback",
			"model":   "test-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "fallback ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	a := &app{
		cfg:        normalizeSeparateModelProfiles(config{TextProvider: "openai", TextBaseURL: upstream.URL}),
		httpClient: upstream.Client(),
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","stream":true,"input":"hi"}`))
	rec := httptest.NewRecorder()

	a.handleOpenAIResponses(rec, req)

	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls)
	}
	if got := rec.Header().Get(responsesFallbackHeader); got != "sync-retry" {
		t.Fatalf("fallback header = %q, want sync-retry", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q, want event stream", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"delta":"fallback ok"`) || !strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("bad fallback stream: %s", body)
	}
}

func TestOpenAIResponsesAdaptsSynchronousNativeResponseWithoutRetry(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["stream"] != true {
			t.Fatalf("first request should prefer streaming: %#v", payload)
		}
		// The provider ignores stream=true and returns a completed JSON object.
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "resp-sync-direct",
			"object":      "response",
			"status":      "completed",
			"model":       "test-model",
			"output_text": "direct sync response",
		})
	}))
	defer upstream.Close()

	a := &app{
		cfg:        normalizeSeparateModelProfiles(config{TextProvider: "openai", TextBaseURL: upstream.URL, TextWireAPI: "responses"}),
		httpClient: upstream.Client(),
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","stream":true,"input":"hi"}`))
	rec := httptest.NewRecorder()

	a.handleOpenAIResponses(rec, req)

	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
	if got := rec.Header().Get(responsesFallbackHeader); got != "sync-response" {
		t.Fatalf("fallback header = %q, want sync-response", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"delta":"direct sync response"`) || !strings.Contains(body, `data: [DONE]`) {
		t.Fatalf("bad direct sync adaptation: %s", body)
	}
}

func TestStreamFallbackDoesNotRetryAuthenticationOrRateLimitErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway} {
		resp := &http.Response{StatusCode: status, Header: make(http.Header)}
		if shouldRetryResponseSynchronously(resp) {
			t.Fatalf("status %d should not be retried synchronously", status)
		}
	}
}
