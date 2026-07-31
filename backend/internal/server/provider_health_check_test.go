package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderHealthCheckConfigDefaultsAndMerges(t *testing.T) {
	defaults := defaultConfig()
	if !providerHealthCheckEnabled(defaults) {
		t.Fatal("provider health checks should be enabled by default")
	}
	if defaults.ProviderHealthCheckIntervalSeconds != defaultProviderHealthCheckIntervalSeconds {
		t.Fatalf("default interval = %d", defaults.ProviderHealthCheckIntervalSeconds)
	}

	disabled := false
	merged := mergeConfig(defaults, config{
		ProviderHealthCheckEnabled:         &disabled,
		ProviderHealthCheckIntervalSeconds: 900,
	})
	if providerHealthCheckEnabled(merged) || merged.ProviderHealthCheckIntervalSeconds != 900 {
		t.Fatalf("merged health settings = enabled:%v interval:%d", providerHealthCheckEnabled(merged), merged.ProviderHealthCheckIntervalSeconds)
	}

	legacy := mergeConfig(defaultConfig(), config{})
	if !providerHealthCheckEnabled(legacy) || legacy.ProviderHealthCheckIntervalSeconds != defaultProviderHealthCheckIntervalSeconds {
		t.Fatalf("legacy config did not inherit defaults: %#v", legacy)
	}
}

func TestNormalizeProviderHealthCheckInterval(t *testing.T) {
	for _, test := range []struct {
		input int
		want  int
	}{
		{0, defaultProviderHealthCheckIntervalSeconds},
		{1, minProviderHealthCheckIntervalSeconds},
		{600, 600},
		{maxProviderHealthCheckIntervalSeconds + 1, maxProviderHealthCheckIntervalSeconds},
	} {
		if got := normalizeProviderHealthCheckIntervalSeconds(test.input); got != test.want {
			t.Errorf("normalize interval %d = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestPeriodicProviderHealthCheckClassifiesHTTPStatusLikeNormalTraffic(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		statusCode      int
		wantFailures    int
		wantLastFailure bool
		wantLastSuccess bool
	}{
		{name: "client error", statusCode: http.StatusTooManyRequests, wantLastSuccess: true},
		{name: "server error", statusCode: http.StatusServiceUnavailable, wantFailures: 1, wantLastFailure: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, http.StatusText(testCase.statusCode), testCase.statusCode)
			}))
			defer upstream.Close()

			cfg := providerRouterTestConfig([]textModelProfile{{
				ID: "checked", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: upstream.URL,
				ModelMappings: []textModelMapping{{Name: "test", Model: "test"}},
			}}, map[string]string{"codex": "checked"})
			enabled := true
			cfg.ProviderHealthCheckEnabled = &enabled
			cfg.ProviderHealthCheckIntervalSeconds = minProviderHealthCheckIntervalSeconds
			router := newProviderRouter()
			now := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
			router.now = func() time.Time { return now }
			a := &app{cfg: cfg, httpClient: upstream.Client(), providerRouter: router}

			if initial := router.claimDueHealthProbes(cfg); len(initial) != 0 {
				t.Fatalf("initial health probes = %#v", initial)
			}
			now = now.Add(time.Duration(minProviderHealthCheckIntervalSeconds) * time.Second)
			due := router.claimDueHealthProbes(cfg)
			if len(due) != 1 {
				t.Fatalf("due health probes = %#v", due)
			}
			a.probeProviderRecovery(context.Background(), due[0])

			status := findProviderStatus(t, a.providerRouterStatus(), "codex", "checked")
			if status.ConsecutiveFailure != testCase.wantFailures || (status.LastFailureAt != nil) != testCase.wantLastFailure || (status.LastSuccessAt != nil) != testCase.wantLastSuccess || status.LastHealthCheckAt == nil {
				t.Fatalf("health status for HTTP %d = %#v", testCase.statusCode, status)
			}
		})
	}
}
