package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAIResponsesStreamingErrorDoesNotRetrySynchronously(t *testing.T) {
	for _, wireAPI := range []string{"chat_completions", "responses"} {
		t.Run(wireAPI, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				wantPath := "/v1/responses"
				if wireAPI == "chat_completions" {
					wantPath = "/v1/chat/completions"
				}
				if r.URL.Path != wantPath {
					t.Errorf("upstream path = %q, want %q", r.URL.Path, wantPath)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				if payload["stream"] != true {
					t.Errorf("streaming Responses request changed: %#v", payload)
				}
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "stream unsupported"}})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp("openai", wireAPI, upstream)
			rec := httptest.NewRecorder()
			a.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true,"input":"hi"}`)))

			if got := calls.Load(); got < 1 {
				t.Fatalf("upstream calls = %d, want at least 1", got)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestOpenAIResponsesRejectsSynchronousJSONForStreamingRequest(t *testing.T) {
	for _, wireAPI := range []string{"chat_completions", "responses"} {
		t.Run(wireAPI, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				wantPath := "/v1/responses"
				if wireAPI == "chat_completions" {
					wantPath = "/v1/chat/completions"
				}
				if r.URL.Path != wantPath {
					t.Errorf("upstream path = %q, want %q", r.URL.Path, wantPath)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				if payload["stream"] != true {
					t.Errorf("streaming Responses attempt changed: %#v", payload)
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"id": "completed-sync", "object": "response", "status": "completed", "output_text": "must not become SSE",
				})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp("openai", wireAPI, upstream)
			rec := httptest.NewRecorder()
			a.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true,"input":"hi"}`)))

			if got := calls.Load(); got < 1 {
				t.Fatalf("upstream calls = %d, want at least 1", got)
			}
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "non-streaming response for a streaming request") {
				t.Fatalf("unexpected response: status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "response.completed") || strings.Contains(rec.Body.String(), "data: [DONE]") {
				t.Fatalf("synchronous response was synthesized into SSE: %s", rec.Body.String())
			}
		})
	}
}

func TestAnthropicCompatibilityStreamingNeverSwitchesOpenAIUpstreamToSync(t *testing.T) {
	t.Run("upstream error", func(t *testing.T) {
		var calls atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.URL.Path != "/v1/chat/completions" {
				t.Errorf("upstream path = %q", r.URL.Path)
			}
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["stream"] != true {
				t.Errorf("Anthropic compatibility stream changed: %#v", payload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
		}))
		defer upstream.Close()

		a := newStreamModeTestApp("openai", "chat_completions", upstream)
		rec := httptest.NewRecorder()
		a.handleAnthropicMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
		if calls.Load() < 1 || rec.Code != http.StatusBadRequest {
			t.Fatalf("unexpected result: calls=%d status=%d body=%s", calls.Load(), rec.Code, rec.Body.String())
		}
	})

	t.Run("successful synchronous JSON", func(t *testing.T) {
		var calls atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode payload: %v", err)
			}
			if r.URL.Path != "/v1/chat/completions" || payload["stream"] != true {
				t.Errorf("Anthropic compatibility attempt changed mode: %s %#v", r.URL.RequestURI(), payload)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "chatcmpl-sync", "choices": []any{map[string]any{"message": map[string]any{"content": "sync"}}},
			})
		}))
		defer upstream.Close()

		a := newStreamModeTestApp("openai", "chat_completions", upstream)
		rec := httptest.NewRecorder()
		a.handleAnthropicMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`)))
		if calls.Load() < 1 || rec.Code != http.StatusBadGateway {
			t.Fatalf("unexpected result: calls=%d status=%d body=%s", calls.Load(), rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "message_start") {
			t.Fatalf("synchronous Chat response was synthesized into Anthropic SSE: %s", rec.Body.String())
		}
	})
}

func TestOpenAIResponsesRealStreamsRemainStreaming(t *testing.T) {
	t.Run("native responses SSE", func(t *testing.T) {
		const streamBody = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}\n\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamBody))
		}))
		defer upstream.Close()
		a := newStreamModeTestApp("openai", "responses", upstream)
		rec := httptest.NewRecorder()
		a.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true,"input":"hi"}`)))
		if rec.Code != http.StatusOK || rec.Body.String() != streamBody {
			t.Fatalf("native Responses stream changed: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("chat completions SSE conversion", func(t *testing.T) {
		const upstreamBody = "data: {\"id\":\"chatcmpl-1\",\"model\":\"test\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(upstreamBody))
		}))
		defer upstream.Close()
		a := newStreamModeTestApp("openai", "chat_completions", upstream)
		rec := httptest.NewRecorder()
		a.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true,"input":"hi"}`)))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `"type":"response.output_text.delta"`) || !strings.Contains(body, `"delta":"hello"`) {
			t.Fatalf("converted Responses stream is invalid: status=%d body=%s", rec.Code, body)
		}
	})
}

func TestOpenAIResponsesSynchronousRequestsRemainSynchronous(t *testing.T) {
	for _, wireAPI := range []string{"chat_completions", "responses"} {
		t.Run(wireAPI, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if payload["stream"] != false {
					t.Errorf("stream = %#v, want false; payload=%#v", payload["stream"], payload)
				}
				if wireAPI == "chat_completions" {
					writeJSON(w, http.StatusOK, map[string]any{
						"id": "chatcmpl-sync", "model": "test",
						"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "sync chat"}, "finish_reason": "stop"}},
					})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"id": "resp-sync", "object": "response", "status": "completed", "output_text": "sync native"})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp("openai", wireAPI, upstream)
			rec := httptest.NewRecorder()
			a.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":false,"input":"hi"}`)))

			if calls.Load() != 1 || rec.Code != http.StatusOK {
				t.Fatalf("unexpected synchronous result: calls=%d status=%d body=%s", calls.Load(), rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") || strings.Contains(rec.Body.String(), "data: [DONE]") {
				t.Fatalf("synchronous response became a stream: headers=%v body=%s", rec.Header(), rec.Body.String())
			}
		})
	}
}

func TestAnthropicCompatibilitySynchronousRequestSetsOpenAIStreamFalse(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["stream"] != false {
			t.Errorf("stream = %#v, want false; payload=%#v", payload["stream"], payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chatcmpl-sync", "model": "test",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "sync"}, "finish_reason": "stop"}},
		})
	}))
	defer upstream.Close()

	a := newStreamModeTestApp("openai", "chat_completions", upstream)
	rec := httptest.NewRecorder()
	a.handleAnthropicMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"test","stream":false,"messages":[]}`)))

	if calls.Load() != 1 || rec.Code != http.StatusOK {
		t.Fatalf("unexpected synchronous result: calls=%d status=%d body=%s", calls.Load(), rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("synchronous response became a stream: headers=%v body=%s", rec.Header(), rec.Body.String())
	}
}
