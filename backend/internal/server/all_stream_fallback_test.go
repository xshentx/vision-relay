package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIChatRetriesSynchronouslyWhenStreamingIsUnsupported(t *testing.T) {
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
		if calls == 1 {
			if payload["stream"] != true {
				t.Fatalf("first request should stream: %#v", payload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
			return
		}
		if payload["stream"] != false {
			t.Fatalf("fallback should be synchronous: %#v", payload)
		}
		if _, ok := payload["stream_options"]; ok {
			t.Fatalf("fallback retained stream_options: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chatcmpl-fallback", "model": "test-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "chat fallback"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4},
		})
	}))
	defer upstream.Close()

	a := newStreamFallbackTestApp("openai", upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	a.handleOpenAIChat(rec, req)

	assertFallbackStream(t, rec, calls, "text/event-stream", `"content":"chat fallback"`)
}

func TestAnthropicNativeRetriesSynchronouslyWhenStreamingIsUnsupported(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if payload["stream"] != true {
				t.Fatalf("first request should stream: %#v", payload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
			return
		}
		if payload["stream"] != false {
			t.Fatalf("fallback should be synchronous: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_fallback", "type": "message", "role": "assistant", "model": "claude-test",
			"content":     []any{map[string]any{"type": "text", "text": "anthropic fallback"}},
			"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 2, "output_tokens": 3},
		})
	}))
	defer upstream.Close()

	a := newStreamFallbackTestApp("anthropic", upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	a.handleAnthropicMessages(rec, req)

	assertFallbackStream(t, rec, calls, "text/event-stream", `"text":"anthropic fallback"`)
}

func TestGeminiRetriesGenerateContentWhenStreamingIsUnsupported(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
				t.Fatalf("first Gemini request was not SSE streaming: %s", r.URL.RequestURI())
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
			return
		}
		if !strings.HasSuffix(r.URL.Path, ":generateContent") || r.URL.Query().Get("alt") != "" {
			t.Fatalf("fallback Gemini request was not synchronous: %s", r.URL.RequestURI())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "gemini fallback"}}}}},
		})
	}))
	defer upstream.Close()

	a := newStreamFallbackTestApp("gemini", upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	rec := httptest.NewRecorder()
	a.handleGeminiGenerate(rec, req)

	assertFallbackStream(t, rec, calls, "text/event-stream", `"text":"gemini fallback"`)
}

func TestOllamaEndpointsRetrySynchronouslyWhenStreamingIsUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		handle     func(*app, http.ResponseWriter, *http.Request)
		wantOutput string
	}{
		{name: "chat", path: "/api/chat", body: `{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`, handle: (*app).handleOllamaChat, wantOutput: `"content":"ollama chat fallback"`},
		{name: "generate", path: "/api/generate", body: `{"model":"test","stream":true,"prompt":"hi"}`, handle: (*app).handleOllamaGenerate, wantOutput: `"response":"ollama generate fallback"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != tc.path {
					t.Fatalf("unexpected upstream path: %s", r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if calls == 1 {
					if payload["stream"] != true {
						t.Fatalf("first request should stream: %#v", payload)
					}
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
					return
				}
				if payload["stream"] != false {
					t.Fatalf("fallback should be synchronous: %#v", payload)
				}
				if tc.name == "chat" {
					writeJSON(w, http.StatusOK, map[string]any{"model": "test", "message": map[string]any{"role": "assistant", "content": "ollama chat fallback"}, "done": true})
				} else {
					writeJSON(w, http.StatusOK, map[string]any{"model": "test", "response": "ollama generate fallback", "done": true})
				}
			}))
			defer upstream.Close()

			a := newStreamFallbackTestApp("ollama", upstream)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handle(a, rec, req)

			assertFallbackStream(t, rec, calls, "application/x-ndjson", tc.wantOutput)
		})
	}
}

func TestSynchronousClientRequestIsNotChangedToStreaming(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["stream"] != false {
			t.Fatalf("sync client request changed: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "chatcmpl-sync", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "sync ok"}}}})
	}))
	defer upstream.Close()

	a := newStreamFallbackTestApp("openai", upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	a.handleOpenAIChat(rec, req)

	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
	if got := rec.Header().Get(responsesFallbackHeader); got != "" {
		t.Fatalf("unexpected fallback header: %q", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"content":"sync ok"`) {
		t.Fatalf("unexpected sync response: %s", body)
	}
}

func newStreamFallbackTestApp(provider string, upstream *httptest.Server) *app {
	return &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: provider,
			TextBaseURL:  upstream.URL,
			TextWireAPI:  "chat_completions",
		}),
		httpClient: upstream.Client(),
	}
}

func assertFallbackStream(t *testing.T, rec *httptest.ResponseRecorder, calls int, wantContentType, wantBody string) {
	t.Helper()
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls)
	}
	if got := rec.Header().Get(responsesFallbackHeader); got != "sync-retry" {
		t.Fatalf("fallback header = %q, want sync-retry", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, wantContentType) {
		t.Fatalf("content type = %q, want %q", got, wantContentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, wantBody) {
		t.Fatalf("response is missing %q: %s", wantBody, body)
	}
}

func TestOpenAIChatPreservesMislabelledSSEWithoutFallback(t *testing.T) {
	calls := 0
	const streamBody = "data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["stream"] != true {
			t.Fatalf("request should remain streaming: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(streamBody))
	}))
	defer upstream.Close()

	a := newStreamFallbackTestApp("openai", upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	a.handleOpenAIChat(rec, req)

	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
	if got := rec.Header().Get(responsesFallbackHeader); got != "" {
		t.Fatalf("mislabelled SSE triggered fallback %q", got)
	}
	if got := rec.Body.String(); got != streamBody {
		t.Fatalf("stream body changed during SSE sniffing:\ngot  %q\nwant %q", got, streamBody)
	}
}
