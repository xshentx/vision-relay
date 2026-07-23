package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderRouterServerErrorDoesNotUseOtherProviders(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	var claudeCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "codex unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "backup"})
	}))
	defer backup.Close()
	claude := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		claudeCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "claude"})
	}))
	defer claude.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "codex-primary", Name: "Codex primary", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: primary.URL},
		{ID: "codex-backup", Name: "Codex backup", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: backup.URL},
		{ID: "claude-primary", Name: "Claude primary", Client: "claude", Provider: "anthropic", BaseURL: claude.URL},
	}, map[string]string{"codex": "codex-primary", "claude": "claude-primary"})
	a := &app{cfg: cfg, httpClient: http.DefaultClient}
	ctx := withProviderRouteContext(context.Background(), providerGroupCodex)
	resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, "codex")), http.MethodPost, "/v1/responses", []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if primaryCalls.Load() != 1 || backupCalls.Load() != 0 || claudeCalls.Load() != 0 {
		t.Fatalf("request escaped active provider: primary=%d backup=%d claude=%d", primaryCalls.Load(), backupCalls.Load(), claudeCalls.Load())
	}
	if got := a.currentConfig().ActiveTextProfileByClient["codex"]; got != "codex-primary" {
		t.Fatalf("runtime failure changed active provider: %q", got)
	}
	selection, ok := providerRouteTraceFromContext(ctx).get()
	if !ok || selection.ProfileID != "codex-primary" || selection.Group != providerGroupCodex {
		t.Fatalf("wrong route trace: %#v, ok=%t", selection, ok)
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "codex-primary")
	if status.FailureCount != 1 || status.ConsecutiveFailure != 1 || status.CircuitState != providerCircuitClosed {
		t.Fatalf("wrong primary state: %#v", status)
	}
}

func TestProviderRouterClientErrorDoesNotTripCircuit(t *testing.T) {
	var backupCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
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
	a := &app{cfg: cfg, httpClient: http.DefaultClient}
	ctx := withProviderRouteContext(context.Background(), providerGroupCodex)
	resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, "codex")), http.MethodPost, "/v1/responses", []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || backupCalls.Load() != 0 {
		t.Fatalf("client error changed route: status=%d backup=%d", resp.StatusCode, backupCalls.Load())
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "primary")
	if status.FailureCount != 0 || status.ConsecutiveFailure != 0 || status.CircuitState != providerCircuitClosed {
		t.Fatalf("client error affected circuit: %#v", status)
	}
}

func TestProviderRouterOpenCircuitShortCircuitsWithoutAlternative(t *testing.T) {
	var primaryCalls atomic.Int64
	var backupCalls atomic.Int64
	var claudeCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backupCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()
	claude := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		claudeCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "claude"})
	}))
	defer claude.Close()

	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "open-primary", Client: "opencode", Provider: "openai", BaseURL: primary.URL},
		{ID: "open-backup", Client: "opencode", Provider: "openai", BaseURL: backup.URL},
		{ID: "claude-primary", Client: "claude", Provider: "anthropic", BaseURL: claude.URL},
	}, map[string]string{"opencode": "open-primary", "claude": "claude-primary"})
	a := &app{cfg: cfg, httpClient: http.DefaultClient}
	for index := 0; index < providerFailureThreshold; index++ {
		resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupOpenCode, "opencode", "/v1/chat/completions")
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("failure %d returned %d", index+1, resp.StatusCode)
		}
		resp.Body.Close()
	}
	shortCircuited := forwardProviderRouterTestRequest(t, a, cfg, providerGroupOpenCode, "opencode", "/v1/chat/completions")
	defer shortCircuited.Body.Close()
	body, _ := io.ReadAll(shortCircuited.Body)
	if shortCircuited.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "provider_circuit_open") {
		t.Fatalf("open circuit response: status=%d body=%s", shortCircuited.StatusCode, body)
	}
	if primaryCalls.Load() != providerFailureThreshold || backupCalls.Load() != 0 {
		t.Fatalf("open circuit accessed an upstream: primary=%d backup=%d", primaryCalls.Load(), backupCalls.Load())
	}
	openStatus := findProviderStatus(t, a.providerRouterStatus(), "opencode", "open-primary")
	if openStatus.CircuitState != providerCircuitOpen || openStatus.FailureCount != providerFailureThreshold || openStatus.OpenUntil == nil {
		t.Fatalf("wrong open circuit state: %#v", openStatus)
	}

	claudeResp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupClaude, "claude", "/v1/messages")
	defer claudeResp.Body.Close()
	if claudeResp.StatusCode != http.StatusOK || claudeCalls.Load() != 1 {
		t.Fatalf("OpenCode circuit affected Claude: status=%d calls=%d", claudeResp.StatusCode, claudeCalls.Load())
	}
	claudeStatus := findProviderStatus(t, a.providerRouterStatus(), "claude", "claude-primary")
	if claudeStatus.CircuitState != providerCircuitClosed || claudeStatus.FailureCount != 0 {
		t.Fatalf("OpenCode state leaked into Claude: %#v", claudeStatus)
	}
	if got := a.currentConfig().ActiveTextProfileByClient["opencode"]; got != "open-primary" {
		t.Fatalf("circuit changed active provider: %q", got)
	}
}

func TestProviderRouterHalfOpenProbeUsesOnlyActiveProvider(t *testing.T) {
	var calls atomic.Int64
	var healthy atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if !healthy.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "active", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
	}}, map[string]string{"codex": "active"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	a := &app{cfg: cfg, httpClient: http.DefaultClient, providerRouter: router}
	for index := 0; index < providerFailureThreshold; index++ {
		resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
		resp.Body.Close()
	}
	healthy.Store(true)
	now = now.Add(providerCircuitCooldown)
	probe := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer probe.Body.Close()
	if probe.StatusCode != http.StatusOK || calls.Load() != providerFailureThreshold+1 {
		t.Fatalf("half-open probe failed: status=%d calls=%d", probe.StatusCode, calls.Load())
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "active")
	if status.CircuitState != providerCircuitClosed || status.ConsecutiveFailure != 0 {
		t.Fatalf("successful probe did not close circuit: %#v", status)
	}
}

func TestProviderRouterStatusDoesNotExposeSecrets(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "codex", Name: "Codex", Client: "codex", Provider: "openai", WireAPI: "responses",
		BaseURL: "https://example.invalid/v1", APIKey: "super-secret",
	}}, map[string]string{"codex": "codex"})
	a := &app{cfg: cfg}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/provider-router/status", nil)
	a.handleProviderRouterStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint returned %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "super-secret") || strings.Contains(strings.ToLower(rec.Body.String()), "api_key") {
		t.Fatalf("status endpoint exposed API key: %s", rec.Body.String())
	}
	var payload providerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	provider := findProviderStatus(t, payload, "codex", "codex")
	if provider.Priority != 1 || !provider.Active || provider.CircuitState != providerCircuitClosed {
		t.Fatalf("wrong provider status: %#v", provider)
	}
}

func TestProviderRouterUnconfiguredGroupDoesNotUseAnotherGroup(t *testing.T) {
	var openCodeCalls atomic.Int64
	openCode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		openCodeCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer openCode.Close()

	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "open-selected", Client: "opencode", Provider: "openai", WireAPI: "chat_completions", BaseURL: openCode.URL,
	}}, map[string]string{"opencode": "open-selected"})
	a := &app{cfg: cfg, httpClient: http.DefaultClient}
	ctx := withProviderRouteContext(context.Background(), providerGroupCodex)
	resp, err := a.forwardRaw(ctx, a.textEndpoint(cfg), http.MethodPost, "/v1/responses", []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "provider_group_unconfigured") {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if openCodeCalls.Load() != 0 {
		t.Fatalf("Codex request escaped into OpenCode group: calls=%d", openCodeCalls.Load())
	}
}

func TestProviderRouterRequestKeepsResolvedSupplierSnapshot(t *testing.T) {
	var firstCalls atomic.Int64
	var secondCalls atomic.Int64
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Errorf("first supplier path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	profiles := []textModelProfile{
		{ID: "codex-first", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: first.URL},
		{ID: "codex-second", Client: "codex", Provider: "openai", WireAPI: "chat_completions", BaseURL: second.URL},
	}
	firstCfg := providerRouterTestConfig(profiles, map[string]string{"codex": "codex-first"})
	a := &app{cfg: firstCfg, httpClient: http.DefaultClient}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = req.WithContext(withProviderRouteContext(req.Context(), providerGroupCodex))

	resolved := a.textConfigForRequest(req)
	if resolved.ActiveTextProfileID != "codex-first" || normalizeWireAPI(resolved.TextWireAPI) != "responses" {
		t.Fatalf("unexpected resolved supplier snapshot: id=%q wire=%q", resolved.ActiveTextProfileID, resolved.TextWireAPI)
	}

	secondCfg := providerRouterTestConfig(profiles, map[string]string{"codex": "codex-second"})
	a.mu.Lock()
	a.cfg = secondCfg
	a.mu.Unlock()

	resp, err := a.forwardRaw(req.Context(), a.textEndpoint(secondCfg), http.MethodPost, "/v1/responses", []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if firstCalls.Load() != 1 || secondCalls.Load() != 0 {
		t.Fatalf("request changed supplier after resolution: first=%d second=%d", firstCalls.Load(), secondCalls.Load())
	}
}

func forwardProviderRouterTestRequest(t *testing.T, a *app, cfg config, group providerGroup, client, path string) *http.Response {
	t.Helper()
	ctx := withProviderRouteContext(context.Background(), group)
	resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, client)), http.MethodPost, path, []byte(`{"model":"requested"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func providerRouterTestConfig(profiles []textModelProfile, active map[string]string) config {
	cfg := config{
		Addr:                      "127.0.0.1:8787",
		TextModelProfiles:         profiles,
		ActiveTextProfileByClient: active,
		VisionModelProfiles:       []visionModelProfile{{ID: "vision", Provider: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini"}},
		ActiveVisionProfileID:     "vision",
	}
	if len(profiles) > 0 {
		cfg.ActiveTextProfileID = profiles[0].ID
		cfg.TextProvider = profiles[0].Provider
		cfg.TextBaseURL = profiles[0].BaseURL
		cfg.TextWireAPI = profiles[0].WireAPI
	}
	return normalizeSeparateModelProfiles(cfg)
}

func findGroupStatus(t *testing.T, status providerStatusResponse, group string) providerGroupStatus {
	t.Helper()
	for _, candidate := range status.Groups {
		if candidate.Group == group {
			return candidate
		}
	}
	t.Fatalf("group %q not found in %#v", group, status)
	return providerGroupStatus{}
}

func findProviderStatus(t *testing.T, status providerStatusResponse, group, profileID string) providerEndpointStatus {
	t.Helper()
	groupStatus := findGroupStatus(t, status, group)
	for _, provider := range groupStatus.Providers {
		if provider.ProfileID == profileID {
			return provider
		}
	}
	t.Fatalf("provider %q not found in group %q: %#v", profileID, group, groupStatus)
	return providerEndpointStatus{}
}

func TestLegacyTextRoutingSurvivesNormalizedConfigRoundTrip(t *testing.T) {
	legacy := config{
		ActiveTextProfileID: "legacy-text",
		TextModelProfiles: []textModelProfile{{
			ID: "legacy-text", Provider: "openai", BaseURL: "https://legacy.example", WireAPI: "chat_completions",
		}},
		ActiveVisionProfileID: "vision",
		VisionModelProfiles: []visionModelProfile{{
			ID: "vision", Provider: "openai", BaseURL: "https://vision.example", Model: "vision-model",
		}},
	}
	normalized := normalizeSeparateModelProfiles(legacy)
	if !normalized.LegacyTextRouting || !normalized.legacyTextRouting {
		t.Fatal("legacy compatibility was not detected")
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var posted config
	if err := json.Unmarshal(raw, &posted); err != nil {
		t.Fatal(err)
	}
	reloaded := normalizeSeparateModelProfiles(posted)
	if !reloaded.LegacyTextRouting || !reloaded.legacyTextRouting {
		t.Fatal("normalized config round-trip disabled legacy compatibility")
	}
	for _, group := range providerGroups {
		candidate, configured := providerRouteCandidateForGroup(reloaded, group, (&app{}).textEndpoint(reloaded))
		if !configured || candidate.Endpoint.BaseURL != "https://legacy.example" {
			t.Fatalf("legacy supplier was not retained for %s: configured=%t candidate=%#v", group, configured, candidate)
		}
	}
}

func TestUnconfiguredTextGroupDoesNotInvokeVisionProvider(t *testing.T) {
	var textCalls atomic.Int64
	var visionCalls atomic.Int64
	textUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		textCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer textUpstream.Close()
	visionUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		visionCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{}})
	}))
	defer visionUpstream.Close()

	visionEnabled := true
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "opencode-only", Client: "opencode", Provider: "openai", BaseURL: textUpstream.URL,
	}}, map[string]string{"opencode": "opencode-only"})
	cfg.VisionEnabled = &visionEnabled
	cfg.VisionModelProfiles = []visionModelProfile{{
		ID: "vision", Provider: "openai", BaseURL: visionUpstream.URL, APIKey: "vision-key", Model: "vision-model",
	}}
	cfg.ActiveVisionProfileID = "vision"
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"codex-model",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]
	}`))
	a.handleRoute(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "provider_group_unconfigured") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if textCalls.Load() != 0 || visionCalls.Load() != 0 {
		t.Fatalf("unconfigured text request reached an upstream: text=%d vision=%d", textCalls.Load(), visionCalls.Load())
	}
	got := a.currentConfig()
	if got.VisionBaseURL != visionUpstream.URL || got.VisionModel != "vision-model" {
		t.Fatalf("text supplier routing changed vision configuration: %#v", got)
	}
}

func TestLegacySharedProxyMigratesToIndependentVisionProfile(t *testing.T) {
	cfg := config{
		ProxyURL:                  "http://legacy-vision-proxy.example",
		ActiveTextProfileID:       "codex",
		ActiveVisionProfileID:     "vision",
		ActiveTextProfileByClient: map[string]string{"codex": "codex"},
		TextModelProfiles: []textModelProfile{{
			ID: "codex", Client: "codex", Provider: "openai",
			BaseURL: "https://text.example", ProxyURL: "http://text-proxy.example",
		}},
		VisionModelProfiles: []visionModelProfile{{
			ID: "vision", Provider: "openai", BaseURL: "https://vision.example", Model: "vision-model",
		}},
	}
	normalized := normalizeSeparateModelProfiles(cfg)
	if got := normalized.VisionModelProfiles[0].ProxyURL; got == nil || *got != "http://legacy-vision-proxy.example" {
		t.Fatalf("legacy shared proxy was not migrated to vision profile: %#v", got)
	}
	if got := (&app{}).visionEndpoint(normalized).ProxyURL; got != "http://legacy-vision-proxy.example" {
		t.Fatalf("vision endpoint proxy = %q", got)
	}
	candidate, configured := providerRouteCandidateForGroup(normalized, providerGroupCodex, (&app{}).textEndpoint(normalized))
	if !configured {
		t.Fatal("codex supplier was not configured")
	}
	if got := candidate.Endpoint.ProxyURL; got != "http://text-proxy.example" {
		t.Fatalf("text endpoint proxy = %q", got)
	}
	if got := (&app{}).visionEndpoint(candidate.Config).ProxyURL; got != "http://legacy-vision-proxy.example" {
		t.Fatalf("text supplier changed migrated vision proxy to %q", got)
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded config
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatal(err)
	}
	reloaded.ProxyURL = "http://different-text-proxy.example"
	reloaded = normalizeSeparateModelProfiles(reloaded)
	if got := (&app{}).visionEndpoint(reloaded).ProxyURL; got != "http://legacy-vision-proxy.example" {
		t.Fatalf("config round-trip coupled vision proxy back to text: %q", got)
	}
}

func TestTextProviderGroupsDoNotAffectVisionRouting(t *testing.T) {
	var textCalls atomic.Int64
	var visionCalls atomic.Int64
	textUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		textCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "text supplier"}}},
		})
	}))
	defer textUpstream.Close()
	visionUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCalls.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("vision path = %q", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "vision supplier"}}},
		})
	}))
	defer visionUpstream.Close()

	profiles := []textModelProfile{
		{ID: "codex", Client: "codex", Provider: "openai", BaseURL: textUpstream.URL, ProxyURL: "http://127.0.0.1:1"},
		{ID: "opencode", Client: "opencode", Provider: "openai", BaseURL: textUpstream.URL, ProxyURL: "http://127.0.0.1:2"},
	}
	cfg := providerRouterTestConfig(profiles, map[string]string{"codex": "codex", "opencode": "opencode"})
	cfg.VisionModelProfiles = []visionModelProfile{{
		ID: "vision", Provider: "openai", BaseURL: visionUpstream.URL,
		APIKey: "vision-key", Model: "vision-model", ProxyURL: stringPtr(""),
	}}
	cfg.ActiveVisionProfileID = "vision"
	cfg = normalizeSeparateModelProfiles(cfg)
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	for i, group := range []providerGroup{providerGroupCodex, providerGroupOpenCode} {
		ctx := withProviderRouteContext(context.Background(), group)
		candidate, configured := a.resolveProviderRoute(ctx, a.textEndpoint(cfg))
		if !configured {
			t.Fatalf("group %s was not configured", group)
		}
		if candidate.Config.ProxyURL == "" {
			t.Fatalf("test setup did not apply the %s text supplier proxy", group)
		}
		if got := a.visionEndpoint(candidate.Config).ProxyURL; got != "" {
			t.Fatalf("group %s changed vision proxy to %q", group, got)
		}
		analysis, err := a.describeImages(ctx, candidate.Config, parsedMessage{
			Text:   fmt.Sprintf("request-%d", i),
			Images: []imageRef{{URL: fmt.Sprintf("https://images.example/%d.png", i), MediaType: "image/png"}},
		})
		if err != nil {
			t.Fatalf("group %s vision request failed: %v", group, err)
		}
		if analysis != "vision supplier" {
			t.Fatalf("group %s used the wrong upstream: %q", group, analysis)
		}
	}
	if got := visionCalls.Load(); got != 2 {
		t.Fatalf("vision upstream calls = %d, want 2", got)
	}
	if got := textCalls.Load(); got != 0 {
		t.Fatalf("vision requests leaked into text supplier routing: calls=%d", got)
	}
}

func TestProviderGroupsPreserveVisionAugmentationByRequestedModelCapability(t *testing.T) {
	protocols := []struct {
		name        string
		client      string
		profileID   string
		wireAPI     string
		path        string
		requestBody string
	}{
		{
			name: "codex responses", client: textProfileClientCodex, profileID: "codex-provider", wireAPI: "responses", path: "/v1/responses",
			requestBody: `{"model":"requested-model","input":[{"role":"user","content":[{"type":"input_text","text":"answer the request"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`,
		},
		{
			name: "opencode chat", client: textProfileClientOpenCode, profileID: "opencode-provider", wireAPI: "chat_completions", path: "/v1/chat/completions",
			requestBody: `{"model":"requested-model","messages":[{"role":"user","content":[{"type":"text","text":"answer the request"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`,
		},
	}

	capabilityCases := []struct {
		name           string
		mappingName    string
		supportsImages bool
		expectVision   bool
	}{
		{name: "text-only uses vision", mappingName: "requested-model", expectVision: true},
		{name: "multimodal bypasses vision", mappingName: "requested-model", supportsImages: true},
		{name: "unmarked model uses vision", mappingName: "different-model", supportsImages: true, expectVision: true},
	}

	for _, protocolCase := range protocols {
		protocolCase := protocolCase
		for _, capabilityCase := range capabilityCases {
			capabilityCase := capabilityCase
			t.Run(protocolCase.name+"/"+capabilityCase.name, func(t *testing.T) {
				var textCalls atomic.Int64
				var visionCalls atomic.Int64
				textPayloads := make(chan map[string]any, 1)
				visionPayloads := make(chan map[string]any, 1)

				textUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					textCalls.Add(1)
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					textPayloads <- payload
					if protocolCase.wireAPI == "responses" {
						writeJSON(w, http.StatusOK, map[string]any{"id": "resp-test", "object": "response", "status": "completed", "output": []any{}})
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{
						"id": "chat-test", "object": "chat.completion",
						"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
					})
				}))
				defer textUpstream.Close()

				visionUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					visionCalls.Add(1)
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					visionPayloads <- payload
					writeJSON(w, http.StatusOK, map[string]any{
						"choices": []any{map[string]any{"message": map[string]any{"content": "VISION_FACT_ONLY"}}},
					})
				}))
				defer visionUpstream.Close()

				visionOn := true
				cfg := providerRouterTestConfig([]textModelProfile{{
					ID: protocolCase.profileID, Client: protocolCase.client, Provider: "openai", WireAPI: protocolCase.wireAPI,
					BaseURL:       textUpstream.URL,
					ModelMappings: []textModelMapping{{Name: capabilityCase.mappingName, Model: "upstream-model", SupportsImages: capabilityCase.supportsImages}},
				}}, map[string]string{protocolCase.client: protocolCase.profileID})
				cfg.VisionEnabled = &visionOn
				cfg.VisionModelProfiles = []visionModelProfile{{
					ID: "vision", Provider: "openai", BaseURL: visionUpstream.URL, APIKey: "vision-key", Model: "vision-model", ProxyURL: stringPtr(""),
				}}
				cfg.ActiveVisionProfileID = "vision"
				cfg = normalizeSeparateModelProfiles(cfg)
				a := &app{cfg: cfg, httpClient: http.DefaultClient}

				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, protocolCase.path, strings.NewReader(protocolCase.requestBody))
				req.Header.Set("Content-Type", "application/json")
				a.handleRoute(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
				}
				if got := textCalls.Load(); got != 1 {
					t.Fatalf("text upstream calls = %d, want 1", got)
				}
				var textPayload map[string]any
				select {
				case textPayload = <-textPayloads:
				case <-time.After(time.Second):
					t.Fatal("text upstream payload was not captured")
				}
				textRaw, _ := json.Marshal(textPayload)
				if !strings.Contains(string(textRaw), `"model":"upstream-model"`) {
					t.Fatalf("grouped supplier did not receive the mapped model: %s", textRaw)
				}

				if !capabilityCase.expectVision {
					if got := visionCalls.Load(); got != 0 {
						t.Fatalf("multimodal text model unexpectedly called vision upstream %d time(s)", got)
					}
					if !strings.Contains(string(textRaw), "data:image/png;base64,aGVsbG8=") {
						t.Fatalf("multimodal text model did not receive the original image: %s", textRaw)
					}
					return
				}

				if got := visionCalls.Load(); got != 1 {
					t.Fatalf("text-only model called vision upstream %d time(s), want 1", got)
				}
				if !strings.Contains(string(textRaw), "VISION_FACT_ONLY") {
					t.Fatalf("text supplier did not receive the vision recognition result: %s", textRaw)
				}
				if strings.Contains(string(textRaw), "data:image/png;base64,aGVsbG8=") {
					t.Fatalf("text-only supplier still received the original image: %s", textRaw)
				}
				var visionPayload map[string]any
				select {
				case visionPayload = <-visionPayloads:
				case <-time.After(time.Second):
					t.Fatal("vision upstream payload was not captured")
				}
				visionRaw, _ := json.Marshal(visionPayload)
				if !strings.Contains(string(visionRaw), `"model":"vision-model"`) || !strings.Contains(string(visionRaw), "data:image/png;base64,aGVsbG8=") {
					t.Fatalf("vision upstream did not receive the configured recognition model and image: %s", visionRaw)
				}
			})
		}
	}
}

func TestProviderRouterCountsResponseBodyReadFailures(t *testing.T) {
	var calls atomic.Int64
	transport := providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          &failingProviderBody{},
			ContentLength: -1,
		}, nil
	})
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "body-failure", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://upstream.invalid",
	}}, map[string]string{"codex": "body-failure"})
	a := &app{cfg: cfg, httpClient: &http.Client{Transport: transport}}

	for index := 0; index < providerFailureThreshold; index++ {
		resp := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
		_, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil || !strings.Contains(readErr.Error(), "body interrupted") {
			t.Fatalf("read %d error = %v", index+1, readErr)
		}
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "body-failure")
	if status.FailureCount != providerFailureThreshold || status.ConsecutiveFailure != providerFailureThreshold || status.CircuitState != providerCircuitOpen {
		t.Fatalf("body failures did not open circuit: %#v", status)
	}
	shortCircuited := forwardProviderRouterTestRequest(t, a, cfg, providerGroupCodex, "codex", "/v1/responses")
	defer shortCircuited.Body.Close()
	if shortCircuited.StatusCode != http.StatusServiceUnavailable || calls.Load() != providerFailureThreshold {
		t.Fatalf("open circuit was not enforced: status=%d calls=%d", shortCircuited.StatusCode, calls.Load())
	}
}

type providerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingProviderBody struct {
	sent bool
}

func (b *failingProviderBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "partial response"), nil
	}
	return 0, errors.New("body interrupted")
}

func (*failingProviderBody) Close() error { return nil }
