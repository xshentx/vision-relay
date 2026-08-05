package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderFailoverAvailabilityFollowsLocalAPI(t *testing.T) {
	enabled := true
	disabled := false
	raw := config{LocalAPIEnabled: &disabled, ProviderFailoverEnabled: &enabled}
	if providerFailoverEnabled(raw) {
		t.Fatal("failover must not run while the local API is disabled")
	}

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://primary.example"},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://backup.example"},
	}, map[string]string{"codex": "primary"})
	cfg.LocalAPIEnabled = &disabled
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	if cfg.ProviderFailoverEnabled == nil || *cfg.ProviderFailoverEnabled {
		t.Fatalf("normalized failover setting = %#v, want false", cfg.ProviderFailoverEnabled)
	}
	if got := cfg.ProviderFailoverProfiles["codex"]; len(got) != 2 || got[0] != "primary" || got[1] != "backup" {
		t.Fatalf("disabled failover should preserve its configured queue: %#v", got)
	}

	cfg.LocalAPIEnabled = &enabled
	cfg.ProviderFailoverEnabled = &enabled
	cfg = normalizeSeparateModelProfiles(cfg)
	if !providerFailoverEnabled(cfg) {
		t.Fatal("failover should be available while the local API is enabled")
	}
}

func TestProviderFailoverUsesNextJoinedProviderOnServerError(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "backup"})
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Name: "Primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "backup", Name: "Backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	ctx := withProviderRouteContext(context.Background(), providerGroupCodex)
	resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, "codex")), http.MethodPost, "/v1/responses", []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || primaryCalls.Load() != 1 || backupCalls.Load() != 1 {
		t.Fatalf("status=%d primary=%d backup=%d body=%s", resp.StatusCode, primaryCalls.Load(), backupCalls.Load(), body)
	}
	selection, ok := providerRouteTraceFromContext(ctx).get()
	if !ok || selection.ProfileID != "backup" {
		t.Fatalf("route trace = %#v, ok=%t", selection, ok)
	}
	primaryStatus := findProviderStatus(t, a.providerRouterStatus(), "codex", "primary")
	if primaryStatus.FailureCount != 1 || primaryStatus.ConsecutiveFailure != 1 {
		t.Fatalf("primary state = %#v", primaryStatus)
	}
}

func TestProviderFailoverEnabledWithoutJoinedProvidersUsesOnlyActive(t *testing.T) {
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || backupCalls.Load() != 0 {
		t.Fatalf("status=%d backup=%d", resp.StatusCode, backupCalls.Load())
	}
}

func TestProviderFailoverStopsAfterFirstSuccessfulProvider(t *testing.T) {
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": "primary"})
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || backupCalls.Load() != 0 {
		t.Fatalf("status=%d backup=%d", resp.StatusCode, backupCalls.Load())
	}
}

func TestProviderFailoverSkipsOpenCircuit(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}
	router := a.textProviderRouter()
	primaryCandidate := providerRouteCandidateForProfile(cfg, providerGroupCodex, cfg.TextModelProfiles[0])
	for index := 0; index < providerFailureThreshold; index++ {
		recordProviderTestFailure(t, router, primaryCandidate, context.DeadlineExceeded)
	}

	resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || primaryCalls.Load() != 0 || backupCalls.Load() != 1 {
		t.Fatalf("status=%d primary=%d backup=%d", resp.StatusCode, primaryCalls.Load(), backupCalls.Load())
	}
}

func TestNormalizeProviderFailoverProfilesFiltersInvalidAndCrossGroupEntries(t *testing.T) {
	profiles := []textModelProfile{
		{ID: "codex-a", Client: "codex"},
		{ID: "codex-b", Client: "codex"},
		{ID: "claude-a", Client: "claude"},
	}
	got := normalizeProviderFailoverProfiles(profiles, map[string][]string{
		"codex": {"codex-b", "", "claude-a", "codex-b", "missing", "codex-a"},
		"other": {"codex-a"},
	})
	if len(got["codex"]) != 2 || got["codex"][0] != "codex-b" || got["codex"][1] != "codex-a" {
		t.Fatalf("codex queue = %#v", got["codex"])
	}
	if len(got["claude"]) != 0 || len(got["other"]) != 0 {
		t.Fatalf("unexpected queues = %#v", got)
	}
}

func TestProviderFailoverQueueExcludesUnjoinedActiveProvider(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "active", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://active.example"},
		{ID: "joined", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://joined.example"},
	}, map[string]string{"codex": "active"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"joined"}}
	cfg = normalizeSeparateModelProfiles(cfg)

	candidates, configured := providerRouteCandidatesForGroup(cfg, providerGroupCodex, endpoint{})
	if !configured || len(candidates) != 1 || candidates[0].ProfileID != "joined" {
		t.Fatalf("candidates = %#v, configured=%t", candidates, configured)
	}
}

func TestProviderFailoverDisabledIgnoresJoinedQueue(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "active", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://active.example"},
		{ID: "joined", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://joined.example"},
	}, map[string]string{"codex": "active"})
	disabled := false
	cfg.ProviderFailoverEnabled = &disabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"joined"}}
	cfg = normalizeSeparateModelProfiles(cfg)

	candidates, configured := providerRouteCandidatesForGroup(cfg, providerGroupCodex, endpoint{})
	if !configured || len(candidates) != 1 || candidates[0].ProfileID != "active" {
		t.Fatalf("candidates = %#v, configured=%t", candidates, configured)
	}
}

func TestProviderFailoverUsesNextProviderOnNetworkError(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	client := &http.Client{Transport: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "primary.example":
			primaryCalls.Add(1)
			return nil, context.DeadlineExceeded
		case "backup.example":
			backupCalls.Add(1)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     "204 No Content",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		default:
			t.Fatalf("unexpected host %q", req.URL.Host)
			return nil, nil
		}
	})}
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://primary.example"},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://backup.example"},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: client}

	resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || primaryCalls.Load() != 1 || backupCalls.Load() != 1 {
		t.Fatalf("status=%d primary=%d backup=%d", resp.StatusCode, primaryCalls.Load(), backupCalls.Load())
	}
}

func TestProviderFailoverUsesNextProviderOnRateLimit(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	client := &http.Client{Transport: providerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "primary.example":
			primaryCalls.Add(1)
			body := "rate limited"
			return &http.Response{
				StatusCode:    http.StatusTooManyRequests,
				Status:        "429 Too Many Requests",
				Header:        http.Header{},
				Body:          io.NopCloser(strings.NewReader(body)),
				ContentLength: int64(len(body)),
				Request:       req,
			}, nil
		case "backup.example":
			backupCalls.Add(1)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     "204 No Content",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		default:
			t.Fatalf("unexpected host %q", req.URL.Host)
			return nil, nil
		}
	})}
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://primary.example"},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://backup.example"},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: client}

	resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || primaryCalls.Load() != 1 || backupCalls.Load() != 1 {
		t.Fatalf("status=%d primary=%d backup=%d", resp.StatusCode, primaryCalls.Load(), backupCalls.Load())
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "primary")
	if status.FailureCount != 1 || status.ConsecutiveFailure != 1 || status.CircuitState != providerCircuitClosed {
		t.Fatalf("rate limit did not count as a retryable provider failure: %#v", status)
	}
}

func TestDefaultConfigDisablesProviderFailover(t *testing.T) {
	cfg := defaultConfig()
	if cfg.ProviderFailoverEnabled == nil || *cfg.ProviderFailoverEnabled {
		t.Fatalf("default failover setting = %#v", cfg.ProviderFailoverEnabled)
	}
	if len(cfg.ProviderFailoverProfiles) != 0 {
		t.Fatalf("default failover queues = %#v", cfg.ProviderFailoverProfiles)
	}
}

func TestProviderFailoverMapsOriginalRequestedModelForEachAttempt(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()

	var backupModel string
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		backupModel = firstString(payload["model"])
		writeJSON(w, http.StatusOK, map[string]any{"id": "backup", "object": "response", "status": "completed", "output": []any{}})
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL, ModelMappings: []textModelMapping{
			{Name: "first", Model: "primary-first"},
			{Name: "second", Model: "primary-second"},
		}},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL, ModelMappings: []textModelMapping{
			{Name: "first", Model: "backup-first"},
			{Name: "second", Model: "backup-second"},
		}},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"second","input":"hi"}`))
	request.Header.Set("Content-Type", "application/json")
	a.handleRoute(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if backupModel != "backup-second" {
		t.Fatalf("backup model = %q, want backup-second", backupModel)
	}
}

func TestProviderFailoverConvertsResponsesRequestForChatCompletionsBackup(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("backup path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["messages"].([]any); !ok {
			t.Fatalf("backup payload was not converted to chat completions: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-backup",
			"object":  "chat.completion",
			"created": 1,
			"model":   firstString(payload["model"]),
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "backup ok",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "chat_completions", BaseURL: backup.URL},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"requested","input":"hi"}`))
	request.Header.Set("Content-Type", "application/json")
	a.handleRoute(recorder, request)
	if recorder.Code != http.StatusOK || primaryCalls.Load() != 1 || backupCalls.Load() != 1 {
		t.Fatalf("status=%d primary=%d backup=%d body=%s", recorder.Code, primaryCalls.Load(), backupCalls.Load(), recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "backup ok") || !strings.Contains(recorder.Body.String(), "response") {
		t.Fatalf("backup chat completion was not converted back to responses: %s", recorder.Body.String())
	}
}

func TestProviderFailoverSkipsImageCapabilityMismatch(t *testing.T) {
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL, ModelMappings: []textModelMapping{{Name: "requested", Model: "primary-model", SupportsImages: true}}},
		{ID: "backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL, ModelMappings: []textModelMapping{{Name: "requested", Model: "backup-model", SupportsImages: false}}},
	}, map[string]string{"codex": "primary"})
	enabled := true
	cfg.ProviderFailoverEnabled = &enabled
	cfg.ProviderFailoverProfiles = map[string][]string{"codex": {"primary", "backup"}}
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	body := `{"model":"requested","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	a.handleRoute(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || backupCalls.Load() != 0 {
		t.Fatalf("status=%d backup calls=%d body=%s", recorder.Code, backupCalls.Load(), recorder.Body.String())
	}
}
