package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesktopActivationHandlerSignalsWithoutBlocking(t *testing.T) {
	activation := make(chan struct{}, 1)
	handler := desktopActivationHandler(activation)

	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/desktop/activate", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("activation status = %d, want %d", response.Code, http.StatusAccepted)
		}
	}

	select {
	case <-activation:
	default:
		t.Fatal("activation signal was not queued")
	}
	select {
	case <-activation:
		t.Fatal("duplicate activation should be coalesced")
	default:
	}
}

func TestActivateExistingDesktopPostsToRunningInstance(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/desktop/activate" || r.Method != http.MethodPost {
			t.Fatalf("unexpected activation request: %s %s", r.Method, r.URL.Path)
		}
		calls++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	if err := activateExistingDesktop(server.URL + "/"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("activation calls = %d, want 1", calls)
	}
}

func TestExistingVisionRelayHealthyRequiresApplicationIdentity(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "vision relay", body: `{"status":"ok","application":"vision-relay"}`, want: true},
		{name: "legacy or unrelated health", body: `{"status":"ok"}`},
		{name: "wrong application", body: `{"status":"ok","application":"other"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Fatalf("unexpected health path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			if got := existingVisionRelayHealthy(server.URL); got != test.want {
				t.Fatalf("healthy = %t, want %t", got, test.want)
			}
		})
	}
}
