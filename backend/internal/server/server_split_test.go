package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultManagementAndRelayAddressesAreIndependent(t *testing.T) {
	cfg := defaultConfig()
	if cfg.ManagementAddr != "127.0.0.1:18473" {
		t.Fatalf("management address = %q, want %q", cfg.ManagementAddr, "127.0.0.1:18473")
	}
	if cfg.Addr != "127.0.0.1:8787" {
		t.Fatalf("relay address = %q, want %q", cfg.Addr, "127.0.0.1:8787")
	}
	if listenPort(cfg.ManagementAddr) == listenPort(cfg.Addr) {
		t.Fatalf("management and relay ports must differ: %#v", cfg)
	}
}

func TestManagementAddressIsFixed(t *testing.T) {
	t.Setenv("VISION_RELAY_MANAGEMENT_ADDR", "127.0.0.1:19999")
	cfg := mergeConfig(defaultConfig(), config{ManagementAddr: "127.0.0.1:19998"})
	if cfg.ManagementAddr != defaultManagementAddr {
		t.Fatalf("merged management address = %q, want fixed %q", cfg.ManagementAddr, defaultManagementAddr)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "management_addr") {
		t.Fatalf("management address must not be serialized as a setting: %s", encoded)
	}

	cfg.ManagementAddr = "127.0.0.1:19997"
	a := &app{cfg: defaultConfig(), configPath: t.TempDir() + "/config.json"}
	if err := a.setConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := a.currentConfig().ManagementAddr; got != defaultManagementAddr {
		t.Fatalf("saved management address = %q, want fixed %q", got, defaultManagementAddr)
	}
}

func TestConfigRejectsRelayOnFixedManagementPort(t *testing.T) {
	cfg := defaultConfig()
	cfg.Addr = "127.0.0.1:18473"
	a := &app{cfg: defaultConfig(), configPath: t.TempDir() + "/config.json"}
	if err := a.setConfig(cfg); err == nil || !strings.Contains(err.Error(), "必须不同") {
		t.Fatalf("same-port config error = %v, want distinct-port validation", err)
	}
}

func TestRequestOriginAlwaysUsesRelayAddress(t *testing.T) {
	cfg := defaultConfig()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18473/api/client/configure", nil)
	if got := requestOrigin(req, cfg); got != "http://127.0.0.1:8787" {
		t.Fatalf("client route origin = %q, want relay API origin", got)
	}
}

func TestManagementAndRelayHandlersAreIsolated(t *testing.T) {
	cfg := defaultConfig()
	a := &app{cfg: cfg}
	management := newManagementHandler(a, make(chan struct{}, 1))
	relay := newRelayHandler(a)

	request := func(handler http.Handler, method, target string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, nil)
		req.RemoteAddr = "127.0.0.1:5000"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	if got := request(management, http.MethodGet, "http://127.0.0.1:18473/").Code; got != http.StatusOK {
		t.Fatalf("management page status = %d, want %d", got, http.StatusOK)
	}
	if got := request(management, http.MethodGet, "http://127.0.0.1:18473/api/config").Code; got != http.StatusOK {
		t.Fatalf("management config status = %d, want %d", got, http.StatusOK)
	}
	if got := request(management, http.MethodGet, "http://127.0.0.1:18473/v1/models").Code; got != http.StatusNotFound {
		t.Fatalf("relay route on management port = %d, want %d", got, http.StatusNotFound)
	}
	if got := request(relay, http.MethodGet, "http://127.0.0.1:8787/").Code; got != http.StatusNotFound {
		t.Fatalf("management page on relay port = %d, want %d", got, http.StatusNotFound)
	}
	if got := request(relay, http.MethodGet, "http://127.0.0.1:8787/api/config").Code; got != http.StatusNotFound {
		t.Fatalf("management API on relay port = %d, want %d", got, http.StatusNotFound)
	}

	managementHealth := request(management, http.MethodGet, "http://127.0.0.1:18473/healthz")
	if !strings.Contains(managementHealth.Body.String(), `"surface":"management"`) {
		t.Fatalf("unexpected management health: %s", managementHealth.Body.String())
	}
	relayHealth := request(relay, http.MethodGet, "http://127.0.0.1:8787/healthz")
	if !strings.Contains(relayHealth.Body.String(), `"surface":"relay"`) {
		t.Fatalf("unexpected relay health: %s", relayHealth.Body.String())
	}
}
func TestManagementAndRelayHandlersServeOnSeparateListeners(t *testing.T) {
	a := &app{cfg: defaultConfig()}
	management := httptest.NewServer(newManagementHandler(a, make(chan struct{}, 1)))
	defer management.Close()
	relay := httptest.NewServer(newRelayHandler(a))
	defer relay.Close()

	tests := []struct {
		name       string
		url        string
		wantStatus int
	}{
		{name: "management page", url: management.URL + "/", wantStatus: http.StatusOK},
		{name: "relay route rejected by management", url: management.URL + "/v1/models", wantStatus: http.StatusNotFound},
		{name: "management page rejected by relay", url: relay.URL + "/", wantStatus: http.StatusNotFound},
		{name: "management API rejected by relay", url: relay.URL + "/api/config", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := http.Get(test.url)
			if err != nil {
				t.Fatalf("GET %s: %v", test.url, err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d", test.url, response.StatusCode, test.wantStatus)
			}
		})
	}
}

func TestManagementRelayStatusProbesRelaySurface(t *testing.T) {
	statusForSurface := func(surface string) int {
		t.Helper()
		relay := httptest.NewServer(healthHandler(surface))
		defer relay.Close()

		cfg := defaultConfig()
		cfg.Addr = strings.TrimPrefix(relay.URL, "http://")
		a := &app{cfg: cfg}
		handler := newManagementHandler(a, make(chan struct{}, 1))
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18473/api/relay/status", nil)
		req.RemoteAddr = "127.0.0.1:5000"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if surface == "relay" && !strings.Contains(recorder.Body.String(), `"online":true`) {
			t.Fatalf("relay status body = %s, want online relay", recorder.Body.String())
		}
		return recorder.Code
	}

	if got := statusForSurface("relay"); got != http.StatusOK {
		t.Fatalf("relay surface status = %d, want %d", got, http.StatusOK)
	}
	if got := statusForSurface("management"); got != http.StatusServiceUnavailable {
		t.Fatalf("management surface status = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestLocalServerURLUsesReachableLoopbackHosts(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{addr: ":8787", want: "http://127.0.0.1:8787/"},
		{addr: "0.0.0.0:8787", want: "http://127.0.0.1:8787/"},
		{addr: "[::]:8787", want: "http://[::1]:8787/"},
		{addr: "[2001:db8::1]:8787", want: "http://[2001:db8::1]:8787/"},
	}
	for _, test := range tests {
		if got := localServerURL(test.addr); got != test.want {
			t.Errorf("localServerURL(%q) = %q, want %q", test.addr, got, test.want)
		}
	}
}
