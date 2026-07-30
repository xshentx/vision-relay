package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStreamingErrorsAreNotRetriedSynchronously(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		path     string
		body     string
		handle   func(*app, http.ResponseWriter, *http.Request)
		validate func(*testing.T, *http.Request, map[string]any)
	}{
		{
			name: "openai chat", provider: "openai", path: "/v1/chat/completions",
			body:   `{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			handle: (*app).handleOpenAIChat,
			validate: func(t *testing.T, r *http.Request, payload map[string]any) {
				if r.URL.Path != "/v1/chat/completions" || payload["stream"] != true {
					t.Errorf("streaming request changed: %s %#v", r.URL.RequestURI(), payload)
				}
			},
		},
		{
			name: "anthropic native", provider: "anthropic", path: "/v1/messages",
			body:   `{"model":"claude-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			handle: (*app).handleAnthropicMessages,
			validate: func(t *testing.T, r *http.Request, payload map[string]any) {
				if r.URL.Path != "/v1/messages" || payload["stream"] != true {
					t.Errorf("streaming request changed: %s %#v", r.URL.RequestURI(), payload)
				}
			},
		},
		{
			name: "gemini", provider: "gemini", path: "/v1beta/models/gemini-test:streamGenerateContent?alt=sse",
			body:   `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			handle: (*app).handleGeminiGenerate,
			validate: func(t *testing.T, r *http.Request, _ map[string]any) {
				if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
					t.Errorf("Gemini streaming endpoint changed: %s", r.URL.RequestURI())
				}
			},
		},
		{
			name: "ollama chat", provider: "ollama", path: "/api/chat",
			body:   `{"model":"test","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			handle: (*app).handleOllamaChat,
			validate: func(t *testing.T, r *http.Request, payload map[string]any) {
				if r.URL.Path != "/api/chat" || payload["stream"] != true {
					t.Errorf("streaming request changed: %s %#v", r.URL.RequestURI(), payload)
				}
			},
		},
		{
			name: "ollama generate", provider: "ollama", path: "/api/generate",
			body:   `{"model":"test","stream":true,"prompt":"hi"}`,
			handle: (*app).handleOllamaGenerate,
			validate: func(t *testing.T, r *http.Request, payload map[string]any) {
				if r.URL.Path != "/api/generate" || payload["stream"] != true {
					t.Errorf("streaming request changed: %s %#v", r.URL.RequestURI(), payload)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode upstream payload: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				tc.validate(t, r, payload)
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stream unsupported"})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp(tc.provider, "", upstream)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handle(a, rec, req)

			if got := calls.Load(); got < 1 {
				t.Fatalf("upstream calls = %d, want at least 1", got)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestSuccessfulSynchronousJSONIsRejectedForStreamingRequest(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		path     string
		body     string
		handle   func(*app, http.ResponseWriter, *http.Request)
	}{
		{name: "openai chat", provider: "openai", path: "/v1/chat/completions", body: `{"model":"test","stream":true,"messages":[]}`, handle: (*app).handleOpenAIChat},
		{name: "anthropic native", provider: "anthropic", path: "/v1/messages", body: `{"model":"test","stream":true,"messages":[]}`, handle: (*app).handleAnthropicMessages},
		{name: "gemini", provider: "gemini", path: "/v1beta/models/gemini-test:streamGenerateContent?alt=sse", body: `{"contents":[]}`, handle: (*app).handleGeminiGenerate},
		{name: "ollama", provider: "ollama", path: "/api/generate", body: `{"model":"test","stream":true,"prompt":"hi"}`, handle: (*app).handleOllamaGenerate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode upstream payload: %v", err)
				}
				if tc.provider == "gemini" {
					if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
						t.Errorf("Gemini attempt changed to synchronous endpoint: %s", r.URL.RequestURI())
					}
				} else if payload["stream"] != true {
					t.Errorf("streaming attempt changed to synchronous mode: %#v", payload)
				}
				writeJSON(w, http.StatusOK, map[string]any{"result": "completed synchronously"})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp(tc.provider, "", upstream)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handle(a, rec, req)

			if got := calls.Load(); got < 1 {
				t.Fatalf("upstream calls = %d, want at least 1", got)
			}
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, "non-streaming response for a streaming request") {
				t.Fatalf("unexpected error response: %s", body)
			}
			if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") || strings.Contains(rec.Header().Get("Content-Type"), "ndjson") {
				t.Fatalf("synchronous JSON was disguised as a stream: %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestRealStreamingResponsesRemainStreaming(t *testing.T) {
	t.Run("mislabelled OpenAI SSE", func(t *testing.T) {
		const streamBody = "data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		var calls atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(streamBody))
		}))
		defer upstream.Close()

		a := newStreamModeTestApp("openai", "", upstream)
		rec := httptest.NewRecorder()
		a.handleOpenAIChat(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`)))

		if calls.Load() != 1 || rec.Body.String() != streamBody {
			t.Fatalf("stream changed or retried: calls=%d body=%q", calls.Load(), rec.Body.String())
		}
	})

	t.Run("Gemini SSE", func(t *testing.T) {
		const streamBody = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamBody))
		}))
		defer upstream.Close()
		a := newStreamModeTestApp("gemini", "", upstream)
		rec := httptest.NewRecorder()
		a.handleGeminiGenerate(rec, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:streamGenerateContent?alt=sse", strings.NewReader(`{"contents":[]}`)))
		if rec.Code != http.StatusOK || rec.Body.String() != streamBody {
			t.Fatalf("Gemini SSE changed: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("Gemini JSON array stream", func(t *testing.T) {
		const streamBody = "[\n{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n]"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(streamBody))
		}))
		defer upstream.Close()
		a := newStreamModeTestApp("gemini", "", upstream)
		rec := httptest.NewRecorder()
		a.handleGeminiGenerate(rec, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:streamGenerateContent", strings.NewReader(`{"contents":[]}`)))
		if rec.Code != http.StatusOK || rec.Body.String() != streamBody {
			t.Fatalf("Gemini JSON array stream changed: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("Ollama NDJSON", func(t *testing.T) {
		const streamBody = "{\"response\":\"hel\",\"done\":false}\n{\"response\":\"lo\",\"done\":true}\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(streamBody))
		}))
		defer upstream.Close()
		a := newStreamModeTestApp("ollama", "", upstream)
		rec := httptest.NewRecorder()
		a.handleOllamaGenerate(rec, httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(`{"model":"test","stream":true,"prompt":"hi"}`)))
		if rec.Code != http.StatusOK || rec.Body.String() != streamBody {
			t.Fatalf("Ollama stream changed: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})
}

func TestSynchronousClientRequestsRemainSynchronous(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		path     string
		body     string
		handle   func(*app, http.ResponseWriter, *http.Request)
		validate func(*testing.T, *http.Request, map[string]any)
	}{
		{name: "openai", provider: "openai", path: "/v1/chat/completions", body: `{"model":"test","stream":false,"messages":[]}`, handle: (*app).handleOpenAIChat, validate: expectPayloadStream(false)},
		{name: "anthropic", provider: "anthropic", path: "/v1/messages", body: `{"model":"test","stream":false,"messages":[]}`, handle: (*app).handleAnthropicMessages, validate: expectPayloadStream(false)},
		{name: "Gemini", provider: "gemini", path: "/v1beta/models/gemini-test:generateContent", body: `{"contents":[]}`, handle: (*app).handleGeminiGenerate, validate: func(t *testing.T, r *http.Request, _ map[string]any) {
			if !strings.HasSuffix(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent") {
				t.Errorf("synchronous Gemini endpoint changed: %s", r.URL.RequestURI())
			}
		}},
		{name: "Ollama", provider: "ollama", path: "/api/generate", body: `{"model":"test","stream":false,"prompt":"hi"}`, handle: (*app).handleOllamaGenerate, validate: expectPayloadStream(false)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				tc.validate(t, r, payload)
				writeJSON(w, http.StatusOK, map[string]any{"mode": "sync"})
			}))
			defer upstream.Close()

			a := newStreamModeTestApp(tc.provider, "", upstream)
			rec := httptest.NewRecorder()
			tc.handle(a, rec, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))

			if got := calls.Load(); got != 1 {
				t.Fatalf("upstream calls = %d, want 1", got)
			}
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"sync"`) {
				t.Fatalf("unexpected synchronous response: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func expectPayloadStream(want bool) func(*testing.T, *http.Request, map[string]any) {
	return func(t *testing.T, _ *http.Request, payload map[string]any) {
		t.Helper()
		if payload["stream"] != want {
			t.Errorf("stream = %#v, want %t; payload=%#v", payload["stream"], want, payload)
		}
	}
}

func newStreamModeTestApp(provider, wireAPI string, upstream *httptest.Server) *app {
	return &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: provider,
			TextBaseURL:  upstream.URL,
			TextWireAPI:  wireAPI,
		}),
		httpClient: upstream.Client(),
	}
}
