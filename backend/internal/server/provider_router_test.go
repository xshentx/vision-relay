package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestProviderCircuitOpensAfterFiveConsecutiveFailures(t *testing.T) {
	if providerFailureThreshold != 5 {
		t.Fatalf("provider failure threshold = %d; want 5", providerFailureThreshold)
	}
	router := newProviderRouter()
	candidate := providerRouteCandidate{Group: providerGroupCodex, ProfileID: "threshold"}
	for failure := 1; failure <= providerFailureThreshold; failure++ {
		router.recordFailure(candidate, errors.New("upstream unavailable"))
		router.mu.Lock()
		state := router.providerStateLocked(candidate.Group, candidate.ProfileID)
		circuitState := state.CircuitState
		consecutiveFailures := state.ConsecutiveFailures
		router.mu.Unlock()
		wantState := providerCircuitClosed
		if failure == providerFailureThreshold {
			wantState = providerCircuitOpen
		}
		if circuitState != wantState || consecutiveFailures != failure {
			t.Fatalf("after failure %d: state=%s consecutive_failures=%d; want state=%s", failure, circuitState, consecutiveFailures, wantState)
		}
	}
}

func TestProviderSuccessAlwaysResetsFailuresAndRestoresCircuit(t *testing.T) {
	for _, failures := range []int{1, providerFailureThreshold - 1, providerFailureThreshold} {
		t.Run(fmt.Sprintf("after_%d_failures", failures), func(t *testing.T) {
			router := newProviderRouter()
			now := time.Date(2026, time.July, 30, 15, 0, 0, 0, time.UTC)
			router.now = func() time.Time { return now }
			candidate := providerRouteCandidate{Group: providerGroupCodex, ProfileID: "recover-on-success"}
			for failure := 0; failure < failures; failure++ {
				router.recordFailure(candidate, errors.New("upstream unavailable"))
			}

			router.recordSuccess(candidate)
			router.mu.Lock()
			state := *router.providerStateLocked(candidate.Group, candidate.ProfileID)
			router.mu.Unlock()
			if state.CircuitState != providerCircuitClosed || state.ConsecutiveFailures != 0 {
				t.Fatalf("successful request did not restore provider: %#v", state)
			}
			if !state.OpenUntil.IsZero() || state.HalfOpenInFlight || state.LastError != "" {
				t.Fatalf("successful request left circuit metadata behind: %#v", state)
			}
			if !state.LastSuccessAt.Equal(now) {
				t.Fatalf("last success time = %v; want %v", state.LastSuccessAt, now)
			}
			if state.FailureCount != int64(failures) {
				t.Fatalf("cumulative failure count = %d; want %d", state.FailureCount, failures)
			}
		})
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

func TestProviderRecoveryProbeClaimsOnlyDueOpenCircuits(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "due", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://due.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
		{ID: "waiting", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://waiting.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
		{ID: "closed", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://closed.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
	}, map[string]string{"codex": "due"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	due := router.providerStateLocked(providerGroupCodex, "due")
	due.CircuitState = providerCircuitOpen
	due.OpenUntil = now
	waiting := router.providerStateLocked(providerGroupCodex, "waiting")
	waiting.CircuitState = providerCircuitOpen
	waiting.OpenUntil = now.Add(time.Second)
	router.providerStateLocked(providerGroupCodex, "closed").CircuitState = providerCircuitClosed
	router.mu.Unlock()

	candidates := router.claimDueRecoveryProbes(cfg)
	if len(candidates) != 1 || candidates[0].ProfileID != "due" || !candidates[0].halfOpen {
		t.Fatalf("claimed recovery probes = %#v", candidates)
	}
	router.mu.Lock()
	if due.CircuitState != providerCircuitHalfOpen || !due.HalfOpenInFlight {
		t.Fatalf("due circuit was not marked as testing: %#v", due)
	}
	if waiting.CircuitState != providerCircuitOpen || waiting.HalfOpenInFlight {
		t.Fatalf("not-yet-due circuit was claimed: %#v", waiting)
	}
	if closed := router.providerStateLocked(providerGroupCodex, "closed"); closed.CircuitState != providerCircuitClosed || closed.HalfOpenInFlight {
		t.Fatalf("closed circuit was claimed: %#v", closed)
	}
	router.mu.Unlock()
	if repeated := router.claimDueRecoveryProbes(cfg); len(repeated) != 0 {
		t.Fatalf("in-flight recovery probe was claimed again: %#v", repeated)
	}
}

func TestProviderHealthCheckClaimsClosedProvidersOnlyAfterIdleInterval(t *testing.T) {
	enabled := true
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "recent", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://recent.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
		{ID: "idle", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://idle.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
	}, map[string]string{"codex": "recent"})
	cfg.ProviderHealthCheckEnabled = &enabled
	cfg.ProviderHealthCheckIntervalSeconds = 300
	router := newProviderRouter()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }

	if initial := router.claimDueHealthProbes(cfg); len(initial) != 0 {
		t.Fatalf("providers were checked immediately after startup: %#v", initial)
	}
	now = now.Add(4*time.Minute + 59*time.Second)
	router.recordSuccess(providerRouteCandidate{Group: providerGroupCodex, ProfileID: "recent"})
	now = now.Add(time.Second)
	candidates := router.claimDueHealthProbes(cfg)
	if len(candidates) != 1 || candidates[0].ProfileID != "idle" || candidates[0].halfOpen {
		t.Fatalf("due periodic health probes = %#v", candidates)
	}
	router.mu.Lock()
	idle := router.providerStateLocked(providerGroupCodex, "idle")
	recent := router.providerStateLocked(providerGroupCodex, "recent")
	if idle.CircuitState != providerCircuitClosed || !idle.HalfOpenInFlight {
		t.Fatalf("idle provider was not claimed without opening its circuit: %#v", idle)
	}
	if recent.HalfOpenInFlight || !recent.LastHealthCheckAt.Equal(now.Add(-time.Second)) {
		t.Fatalf("recent request did not postpone its health check: %#v", recent)
	}
	router.mu.Unlock()
}

func TestProviderHealthCheckCanBeDisabledWithoutDisablingCircuitRecovery(t *testing.T) {
	disabled := false
	cfg := providerRouterTestConfig([]textModelProfile{
		{ID: "closed", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://closed.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
		{ID: "open", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://open.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}}},
	}, map[string]string{"codex": "closed"})
	cfg.ProviderHealthCheckEnabled = &disabled
	cfg.ProviderHealthCheckIntervalSeconds = 60
	router := newProviderRouter()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	closed := router.providerStateLocked(providerGroupCodex, "closed")
	closed.LastHealthCheckAt = now.Add(-time.Hour)
	open := router.providerStateLocked(providerGroupCodex, "open")
	open.CircuitState = providerCircuitOpen
	open.OpenUntil = now
	router.mu.Unlock()

	candidates := router.claimDueHealthProbes(cfg)
	if len(candidates) != 1 || candidates[0].ProfileID != "open" || !candidates[0].halfOpen {
		t.Fatalf("disabled active checks should claim only circuit recovery: %#v", candidates)
	}
	if closed.HalfOpenInFlight {
		t.Fatalf("closed provider was checked while disabled: %#v", closed)
	}
}

func TestPeriodicProviderHealthFailureUsesNormalFailureThreshold(t *testing.T) {
	enabled := true
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "periodic", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://periodic.invalid",
		ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
	}}, map[string]string{"codex": "periodic"})
	cfg.ProviderHealthCheckEnabled = &enabled
	cfg.ProviderHealthCheckIntervalSeconds = 60
	router := newProviderRouter()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	if got := router.claimDueHealthProbes(cfg); len(got) != 0 {
		t.Fatalf("unexpected initial probes: %#v", got)
	}
	now = now.Add(time.Minute)
	candidate := router.claimDueHealthProbes(cfg)[0]
	router.recordFailure(candidate, errors.New("periodic check failed"))

	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "periodic")
	if state.CircuitState != providerCircuitClosed || state.ConsecutiveFailures != 1 || !state.LastHealthCheckAt.Equal(now) {
		t.Fatalf("one periodic failure should not immediately open the circuit: %#v", state)
	}
	router.mu.Unlock()
}

func TestProviderSuccessCancelsRecoveryProbeAndIgnoresItsLateFailure(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "recovering", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://recovering.invalid",
		ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
	}}, map[string]string{"codex": "recovering"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "recovering")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()

	candidates := router.claimDueRecoveryProbes(cfg)
	if len(candidates) != 1 {
		t.Fatalf("recovery probe candidates = %#v", candidates)
	}
	probeContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !router.attachRecoveryProbe(candidates[0], cancel) {
		t.Fatal("failed to attach claimed recovery probe")
	}

	// This represents any other successful request for the same provider.
	now = now.Add(time.Second)
	router.recordSuccess(providerRouteCandidate{Group: providerGroupCodex, ProfileID: "recovering"})
	select {
	case <-probeContext.Done():
	default:
		t.Fatal("successful request did not cancel the active recovery probe")
	}

	router.mu.Lock()
	restored := *router.providerStateLocked(providerGroupCodex, "recovering")
	router.mu.Unlock()
	if restored.CircuitState != providerCircuitClosed || restored.ConsecutiveFailures != 0 || restored.HalfOpenInFlight || restored.RecoveryProbeCancel != nil {
		t.Fatalf("successful request did not fully restore provider: %#v", restored)
	}
	if !restored.LastSuccessAt.Equal(now) {
		t.Fatalf("last success time = %v; want %v", restored.LastSuccessAt, now)
	}

	// A transport that does not stop promptly may still return an error. The
	// old probe generation must not reopen the circuit after the success.
	router.recordFailure(candidates[0], errors.New("late recovery probe failure"))
	router.releaseHalfOpenProbe(candidates[0])
	router.mu.Lock()
	afterLateResult := *router.providerStateLocked(providerGroupCodex, "recovering")
	router.mu.Unlock()
	if afterLateResult.CircuitState != providerCircuitClosed || afterLateResult.ConsecutiveFailures != 0 || afterLateResult.FailureCount != restored.FailureCount {
		t.Fatalf("late recovery result changed restored provider: %#v", afterLateResult)
	}
}

func TestProviderConfigChangeCancelsRecoveryProbeAndIgnoresLateResult(t *testing.T) {
	oldConfig := providerRouterTestConfig([]textModelProfile{{
		ID: "changing", Client: "codex", Provider: "openai", WireAPI: "responses",
		BaseURL: "https://old.invalid", APIKey: "old-key",
		ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
	}}, map[string]string{"codex": "changing"})

	for _, testCase := range []struct {
		name      string
		newConfig config
	}{
		{
			name: "edited",
			newConfig: providerRouterTestConfig([]textModelProfile{{
				ID: "changing", Client: "codex", Provider: "openai", WireAPI: "responses",
				BaseURL: "https://new.invalid", APIKey: "new-key",
				ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
			}}, map[string]string{"codex": "changing"}),
		},
		{
			name:      "deleted",
			newConfig: providerRouterTestConfig(nil, nil),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := newProviderRouter()
			now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
			router.now = func() time.Time { return now }
			router.mu.Lock()
			state := router.providerStateLocked(providerGroupCodex, "changing")
			state.CircuitState = providerCircuitOpen
			state.ConsecutiveFailures = providerFailureThreshold
			state.FailureCount = providerFailureThreshold
			state.OpenUntil = now
			router.mu.Unlock()

			candidates := router.claimDueRecoveryProbes(oldConfig)
			if len(candidates) != 1 {
				t.Fatalf("recovery probe candidates = %#v", candidates)
			}
			probeContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !router.attachRecoveryProbe(candidates[0], cancel) {
				t.Fatal("failed to attach claimed recovery probe")
			}

			a := &app{
				cfg:            oldConfig,
				providerRouter: router,
				configPath:     filepath.Join(t.TempDir(), "config.json"),
			}
			if err := a.setConfig(testCase.newConfig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-probeContext.Done():
			case <-time.After(time.Second):
				t.Fatal("configuration change did not cancel the stale recovery probe")
			}

			router.mu.Lock()
			restored := *router.providerStateLocked(providerGroupCodex, "changing")
			router.mu.Unlock()
			if restored.CircuitState != providerCircuitClosed || restored.ConsecutiveFailures != 0 ||
				restored.FailureCount != 0 || restored.HalfOpenInFlight || restored.RecoveryProbeCancel != nil {
				t.Fatalf("configuration change did not reset provider runtime state: %#v", restored)
			}

			// A stale transport may ignore cancellation and complete later. Neither
			// outcome may mutate the replacement provider's clean runtime state.
			router.recordFailure(candidates[0], errors.New("late stale probe failure"))
			router.recordSuccess(candidates[0])
			router.mu.Lock()
			afterLateResults := *router.providerStateLocked(providerGroupCodex, "changing")
			router.mu.Unlock()
			if afterLateResults.CircuitState != providerCircuitClosed || afterLateResults.ConsecutiveFailures != 0 ||
				afterLateResults.FailureCount != 0 || !afterLateResults.LastSuccessAt.IsZero() {
				t.Fatalf("late stale probe result changed replacement provider state: %#v", afterLateResults)
			}
		})
	}
}

func TestProviderNameChangeKeepsRecoveryProbe(t *testing.T) {
	oldConfig := providerRouterTestConfig([]textModelProfile{{
		ID: "renamed", Name: "Old name", Client: "codex", Provider: "openai", WireAPI: "responses",
		BaseURL: "https://same.invalid", ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
	}}, map[string]string{"codex": "renamed"})
	newConfig := oldConfig
	newConfig.TextModelProfiles = append([]textModelProfile(nil), oldConfig.TextModelProfiles...)
	newConfig.TextModelProfiles[0].Name = "New name"

	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "renamed")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()
	candidate := router.claimDueRecoveryProbes(oldConfig)[0]
	probeContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !router.attachRecoveryProbe(candidate, cancel) {
		t.Fatal("failed to attach claimed recovery probe")
	}

	router.reconcileConfigChange(oldConfig, newConfig)
	select {
	case <-probeContext.Done():
		t.Fatal("display-name-only change canceled an otherwise valid recovery probe")
	default:
	}
	router.mu.Lock()
	unchanged := *router.providerStateLocked(providerGroupCodex, "renamed")
	router.mu.Unlock()
	if unchanged.CircuitState != providerCircuitHalfOpen || !unchanged.HalfOpenInFlight || unchanged.RecoveryProbeCancel == nil {
		t.Fatalf("display-name-only change reset recovery state: %#v", unchanged)
	}
}

func TestProviderWithoutModelMappingsRecoversUsingLastRequestedModel(t *testing.T) {
	var healthy atomic.Bool
	var probeCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		probeCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode probe body: %v", err)
		}
		if payload["model"] != "dynamic-model" || payload["input"] != "hi" {
			t.Errorf("probe payload = %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{"output_text": "ok"})
	}))
	defer upstream.Close()

	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "dynamic", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
	}}, map[string]string{"codex": "dynamic"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	a := &app{cfg: cfg, httpClient: http.DefaultClient, providerRouter: router}

	for attempt := 0; attempt < providerFailureThreshold; attempt++ {
		ctx := withProviderRouteContext(context.Background(), providerGroupCodex)
		resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, "codex")), http.MethodPost,
			"/v1/responses", []byte(`{"model":"dynamic-model"}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("failure %d returned %d", attempt+1, resp.StatusCode)
		}
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "dynamic")
	if status.CircuitState != providerCircuitOpen || status.ConsecutiveFailure != providerFailureThreshold {
		t.Fatalf("provider did not trip after failures: %#v", status)
	}

	healthy.Store(true)
	now = now.Add(providerCircuitCooldown)
	candidates := router.claimDueRecoveryProbes(cfg)
	if len(candidates) != 1 || candidates[0].recoveryProbeModel != "dynamic-model" {
		t.Fatalf("dynamic recovery candidates = %#v", candidates)
	}
	a.probeProviderRecovery(context.Background(), candidates[0])

	status = findProviderStatus(t, a.providerRouterStatus(), "codex", "dynamic")
	if probeCalls.Load() != 1 || status.CircuitState != providerCircuitClosed || status.ConsecutiveFailure != 0 {
		t.Fatalf("dynamic-model recovery did not restore provider: calls=%d status=%#v", probeCalls.Load(), status)
	}
}

func TestLegacyProviderGroupRecoveryProbeRestoresCircuit(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Errorf("legacy probe path = %q", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"output_text": "ok"})
	}))
	defer upstream.Close()

	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "global", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
		ModelMappings: []textModelMapping{{Name: "legacy-model", Model: "legacy-model"}},
	}}, map[string]string{"codex": "global"})
	cfg.LegacyTextRouting = true
	cfg.legacyTextRouting = true

	candidate, configured := providerRouteCandidateForGroup(cfg, providerGroupOpenCode, (&app{}).textEndpoint(cfg))
	if !configured || candidate.ProfileID != "legacy-opencode" {
		t.Fatalf("legacy route candidate = %#v, configured=%t", candidate, configured)
	}
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupOpenCode, candidate.ProfileID)
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()
	a := &app{cfg: cfg, httpClient: http.DefaultClient, providerRouter: router}

	candidates := router.claimDueRecoveryProbes(cfg)
	if len(candidates) != 1 || candidates[0].ProfileID != candidate.ProfileID || candidates[0].recoveryProbeModel != "legacy-model" {
		t.Fatalf("legacy recovery candidates = %#v", candidates)
	}
	a.probeProviderRecovery(context.Background(), candidates[0])

	router.mu.Lock()
	restored := *router.providerStateLocked(providerGroupOpenCode, candidate.ProfileID)
	router.mu.Unlock()
	if calls.Load() != 1 || restored.CircuitState != providerCircuitClosed || restored.ConsecutiveFailures != 0 {
		t.Fatalf("legacy recovery did not restore provider: calls=%d state=%#v", calls.Load(), restored)
	}
}

func TestProviderWithoutAnyKnownModelWaitsForRoutedRequest(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "unknown-model", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://unknown.invalid",
	}}, map[string]string{"codex": "unknown-model"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "unknown-model")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.FailureCount = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()

	if candidates := router.claimDueRecoveryProbes(cfg); len(candidates) != 0 {
		t.Fatalf("provider without a known model was probed: %#v", candidates)
	}
	router.mu.Lock()
	unchanged := *router.providerStateLocked(providerGroupCodex, "unknown-model")
	router.mu.Unlock()
	if unchanged.CircuitState != providerCircuitOpen || unchanged.HalfOpenInFlight ||
		unchanged.ConsecutiveFailures != providerFailureThreshold || unchanged.FailureCount != providerFailureThreshold {
		t.Fatalf("local probe construction failure changed provider state: %#v", unchanged)
	}
}

func TestProviderRecoveryProbeClosesCircuitAfterUpstreamRecovers(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Errorf("probe path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode probe body: %v", err)
		}
		if payload["model"] != "recovery-model" || payload["input"] != "hi" {
			t.Errorf("probe payload = %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{"output_text": "ok"})
	}))
	defer upstream.Close()

	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "recovering", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
		ModelMappings: []textModelMapping{{Name: "recovery", Model: "recovery-model"}},
	}}, map[string]string{"codex": "recovering"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "recovering")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()
	a := &app{cfg: cfg, httpClient: http.DefaultClient, providerRouter: router}

	candidates := router.claimDueRecoveryProbes(cfg)
	if len(candidates) != 1 {
		t.Fatalf("recovery probe candidates = %#v", candidates)
	}
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "recovering")
	if status.CircuitState != providerCircuitHalfOpen {
		t.Fatalf("claimed probe status = %#v", status)
	}
	a.probeProviderRecovery(context.Background(), candidates[0])

	status = findProviderStatus(t, a.providerRouterStatus(), "codex", "recovering")
	if calls.Load() != 1 || status.CircuitState != providerCircuitClosed || status.ConsecutiveFailure != 0 || status.LastSuccessAt == nil {
		t.Fatalf("successful recovery probe did not restore provider: calls=%d status=%#v", calls.Load(), status)
	}
}

func TestProviderRecoveryProbeFailureSchedulesNextInterval(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "still down", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "down", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
		ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
	}}, map[string]string{"codex": "down"})
	router := newProviderRouter()
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	router.now = func() time.Time { return now }
	router.mu.Lock()
	state := router.providerStateLocked(providerGroupCodex, "down")
	state.CircuitState = providerCircuitOpen
	state.ConsecutiveFailures = providerFailureThreshold
	state.OpenUntil = now
	router.mu.Unlock()
	a := &app{cfg: cfg, httpClient: http.DefaultClient, providerRouter: router}

	candidate := router.claimDueRecoveryProbes(cfg)[0]
	a.probeProviderRecovery(context.Background(), candidate)
	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "down")
	if status.CircuitState != providerCircuitOpen || status.OpenUntil == nil || !status.OpenUntil.Equal(now.Add(providerCircuitCooldown)) {
		t.Fatalf("failed recovery probe did not schedule next interval: %#v", status)
	}
	if repeated := router.claimDueRecoveryProbes(cfg); len(repeated) != 0 {
		t.Fatalf("failed provider was probed again before cooldown: %#v", repeated)
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
		provider    string
		wireAPI     string
		path        string
		requestBody string
	}{
		{
			name: "codex responses", client: textProfileClientCodex, profileID: "codex-provider", provider: "openai", wireAPI: "responses", path: "/v1/responses",
			requestBody: `{"model":"requested-model","input":[{"role":"user","content":[{"type":"input_text","text":"answer the request"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`,
		},
		{
			name: "claude messages", client: textProfileClientClaude, profileID: "claude-provider", provider: "anthropic", path: "/v1/messages",
			requestBody: `{"model":"requested-model","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"answer the request"},{"type":"image","source":{"type":"url","url":"data:image/png;base64,aGVsbG8="}}]}]}`,
		},
		{
			name: "opencode chat", client: textProfileClientOpenCode, profileID: "opencode-provider", provider: "openai", wireAPI: "chat_completions", path: "/v1/chat/completions",
			requestBody: `{"model":"requested-model","messages":[{"role":"user","content":[{"type":"text","text":"answer the request"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`,
		},
		{
			name: "openclaw chat", client: textProfileClientOpenClaw, profileID: "openclaw-provider", provider: "openai", wireAPI: "chat_completions", path: "/openclaw/v1/chat/completions",
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
					ID: protocolCase.profileID, Client: protocolCase.client, Provider: protocolCase.provider, WireAPI: protocolCase.wireAPI,
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
