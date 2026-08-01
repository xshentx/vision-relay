package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestHandleModelTestSuccessDoesNotRestoreOpenCircuit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"output_text": "ok"})
	}))
	defer upstream.Close()

	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "profile-1", Name: "Test provider", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
		ModelMappings: []textModelMapping{{Name: "gpt-test", Model: "gpt-test"}},
	}}, map[string]string{"codex": "profile-1"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "profile-1")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now.Add(providerCircuitCooldown)
	router.mu.Unlock()

	a := &app{cfg: cfg, httpClient: upstream.Client(), providerRouter: router}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"gpt-test","prompt":"hi"}`))
	a.handleModelTest(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "profile-1")
	if status.CircuitState != providerCircuitOpen || status.ConsecutiveFailure != providerFailureThreshold || status.LastSuccessAt != nil {
		t.Fatalf("manual model test changed real-traffic circuit state: %#v", status)
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
			ID: "profile-1", Client: "codex", Provider: "openai", BaseURL: upstream.URL,
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
	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].Protocol != "\u6a21\u578b\u6d4b\u8bd5" || logs[0].Status != http.StatusTooManyRequests || !strings.Contains(logs[0].Error, "rate limited") {
		t.Fatalf("failed model test was not logged correctly: %#v", logs)
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "profile-1")
	if status.ConsecutiveFailure != 0 || status.LastFailureAt != nil || status.LastSuccessAt != nil {
		t.Fatalf("manual model test changed real-traffic circuit state: %#v", status)
	}
}

func TestHandleModelTestDoesNotCountCanceledRequestAsProviderFailure(t *testing.T) {
	a := &app{
		cfg: config{TextModelProfiles: []textModelProfile{{
			ID: "profile-1", Client: "codex", Provider: "openai", BaseURL: "https://example.invalid",
			ModelMappings: []textModelMapping{{Name: "gpt-test", Model: "gpt-test"}},
		}}},
		httpClient: &http.Client{Transport: providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"gpt-test","prompt":"hi"}`)).WithContext(ctx)
	recorder := httptest.NewRecorder()
	a.handleModelTest(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "profile-1")
	if status.ConsecutiveFailure != 0 || status.LastFailureAt != nil || status.LastSuccessAt != nil {
		t.Fatalf("canceled model test should not change provider health: %#v", status)
	}
}

func TestHandleModelTestServerErrorDoesNotAffectCircuit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	a := &app{
		cfg: config{TextModelProfiles: []textModelProfile{{
			ID: "profile-1", Client: "codex", Provider: "openai", BaseURL: upstream.URL,
			ModelMappings: []textModelMapping{{Name: "gpt-test", Model: "gpt-test"}},
		}}},
		httpClient: upstream.Client(),
	}
	recorder := httptest.NewRecorder()
	a.handleModelTest(recorder, httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"gpt-test","prompt":"hi"}`)))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "profile-1")
	if status.ConsecutiveFailure != 0 || status.LastFailureAt != nil || status.LastSuccessAt != nil {
		t.Fatalf("manual model test changed real-traffic circuit state: %#v", status)
	}
}

func TestHandleModelTestLogsUsageAndAddsItToDashboard(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"model":   "gpt-test",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10},
		})
	}))
	defer upstream.Close()

	a := &app{
		cfg: config{TextModelProfiles: []textModelProfile{{
			ID: "profile-1", Name: "Test provider", Provider: "openai", BaseURL: upstream.URL,
			ModelMappings: []textModelMapping{{Name: "gpt-test", Model: "gpt-test"}},
		}}},
		httpClient: upstream.Client(),
	}
	recorder := httptest.NewRecorder()
	a.handleModelTest(recorder, httptest.NewRequest(http.MethodPost, "/api/model-test", strings.NewReader(`{"profile_id":"profile-1","model":"gpt-test","prompt":"hi"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	logs := a.currentLogs()
	if len(logs) != 1 {
		t.Fatalf("model test logs = %d, want 1", len(logs))
	}
	log := logs[0]
	if log.Protocol != "\u6a21\u578b\u6d4b\u8bd5" || log.Path != "/api/model-test" || log.UpstreamName != "Test provider" || log.Model != "gpt-test" {
		t.Fatalf("unexpected model test log identity: %#v", log)
	}
	if log.InputTokens != 7 || log.OutputTokens != 3 || log.TotalTokens != 10 {
		t.Fatalf("unexpected model test usage: %#v", log)
	}

	dashboardRecorder := httptest.NewRecorder()
	a.handleDashboard(dashboardRecorder, httptest.NewRequest(http.MethodGet, "/api/dashboard?period=all", nil))
	if dashboardRecorder.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", dashboardRecorder.Code, dashboardRecorder.Body.String())
	}
	var dashboard dashboardResponse
	if err := json.NewDecoder(dashboardRecorder.Body).Decode(&dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Summary.Requests != 1 || dashboard.Summary.PeriodTokens != 10 || dashboard.Summary.LifetimeTokens != 10 {
		t.Fatalf("model test usage was not included in the dashboard: %#v", dashboard.Summary)
	}
}
