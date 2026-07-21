package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleModelTestProviders(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		wireAPI      string
		model        string
		wantPath     string
		responseBody string
		wantOutput   string
		checkAuth    func(*testing.T, *http.Request)
	}{
		{
			name: "openai chat completions", provider: "openai", wireAPI: "chat_completions", model: "gpt-test",
			wantPath: "/v1/chat/completions", responseBody: `{"choices":[{"message":{"role":"assistant","content":"chat ok"}}]}`, wantOutput: "chat ok",
			checkAuth: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Errorf("Authorization = %q", got)
				}
			},
		},
		{
			name: "openai responses", provider: "openai", wireAPI: "responses", model: "gpt-response",
			wantPath: "/v1/responses", responseBody: `{"output":[{"type":"message","content":[{"type":"output_text","text":"responses ok"}]}]}`, wantOutput: "responses ok",
			checkAuth: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Errorf("Authorization = %q", got)
				}
			},
		},
		{
			name: "anthropic messages", provider: "anthropic", model: "claude-test",
			wantPath: "/v1/messages", responseBody: `{"content":[{"type":"text","text":"anthropic ok"}]}`, wantOutput: "anthropic ok",
			checkAuth: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("x-api-key"); got != "test-key" {
					t.Errorf("x-api-key = %q", got)
				}
				if got := r.Header.Get("anthropic-version"); got == "" {
					t.Error("anthropic-version is empty")
				}
			},
		},
		{
			name: "gemini generate content", provider: "gemini", model: "gemini-test",
			wantPath: "/v1beta/models/gemini-test:generateContent", responseBody: `{"candidates":[{"content":{"parts":[{"text":"gemini ok"}]}}]}`, wantOutput: "gemini ok",
			checkAuth: func(t *testing.T, r *http.Request) {
				if got := r.URL.Query().Get("key"); got != "test-key" {
					t.Errorf("key = %q", got)
				}
			},
		},
		{
			name: "ollama chat", provider: "ollama", model: "llama-test",
			wantPath: "/api/chat", responseBody: `{"message":{"role":"assistant","content":"ollama ok"}}`, wantOutput: "ollama ok",
			checkAuth: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Errorf("Authorization = %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				if r.URL.Path != tt.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.wantPath)
				}
				tt.checkAuth(t, r)
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(body, []byte("hi")) {
					t.Errorf("request does not contain default prompt: %s", body)
				}
				if tt.provider != "gemini" && !bytes.Contains(body, []byte(tt.model)) {
					t.Errorf("request does not contain model: %s", body)
				}
				w.Header().Set("X-Request-ID", "request-123")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.responseBody)
			}))
			defer upstream.Close()

			a := &app{
				cfg: config{TextModelProfiles: []textModelProfile{{
					ID: "profile-1", Name: "Test provider", Provider: tt.provider, BaseURL: upstream.URL,
					APIKey: "test-key", WireAPI: tt.wireAPI,
					ModelMappings: []textModelMapping{{Name: "Friendly name", Model: tt.model}},
				}}},
				httpClient: upstream.Client(),
			}
			req := httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"Friendly name","prompt":""}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			a.handleModelTest(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var result modelTestResult
			if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if !result.OK || result.Output != tt.wantOutput {
				t.Errorf("result = %#v", result)
			}
			if result.Model != tt.model {
				t.Errorf("model = %q, want %q", result.Model, tt.model)
			}
			if result.RequestID != "request-123" {
				t.Errorf("request id = %q", result.RequestID)
			}
		})
	}
}

func TestHandleModelTestRejectsUnconfiguredModel(t *testing.T) {
	a := &app{cfg: config{TextModelProfiles: []textModelProfile{{
		ID: "profile-1", Provider: "openai", ModelMappings: []textModelMapping{{Name: "Allowed", Model: "allowed-model"}},
	}}}}
	req := httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"other-model","prompt":"hi"}`))
	recorder := httptest.NewRecorder()
	a.handleModelTest(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleModelTestReportsUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "failed-request")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{TextModelProfiles: []textModelProfile{{
			ID: "profile-1", Provider: "openai", BaseURL: upstream.URL,
			ModelMappings: []textModelMapping{{Name: "gpt-test", Model: "gpt-test"}},
		}}},
		httpClient: upstream.Client(),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"gpt-test","prompt":"hi"}`))
	recorder := httptest.NewRecorder()
	a.handleModelTest(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		UpstreamStatus int    `json:"upstream_status"`
		RequestID      string `json:"request_id"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.UpstreamStatus != http.StatusTooManyRequests {
		t.Errorf("upstream status = %d", payload.UpstreamStatus)
	}
	if payload.RequestID != "failed-request" {
		t.Errorf("request id = %q", payload.RequestID)
	}
	if !strings.Contains(payload.Error.Message, "rate limited") {
		t.Errorf("error = %q", payload.Error.Message)
	}
}
