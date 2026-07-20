package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementAccessControl(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := withManagementAccess(withCORS(next))
	tests := []struct {
		name       string
		path       string
		remoteAddr string
		host       string
		origin     string
		wantStatus int
		wantCORS   string
	}{
		{name: "remote management API is rejected", path: "/api/config", remoteAddr: "192.0.2.10:5000", host: "127.0.0.1:8787", wantStatus: http.StatusForbidden},
		{name: "remote management page is rejected", path: "/", remoteAddr: "192.0.2.10:5000", host: "127.0.0.1:8787", wantStatus: http.StatusForbidden},
		{name: "non-loopback host is rejected", path: "/api/config", remoteAddr: "127.0.0.1:5000", host: "relay.example:8787", wantStatus: http.StatusForbidden},
		{name: "cross-origin management request is rejected", path: "/api/config", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:8787", origin: "https://attacker.example", wantStatus: http.StatusForbidden},
		{name: "local same-origin management request is allowed", path: "/api/config", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", wantStatus: http.StatusOK},
		{name: "native proxy API remains remotely accessible", path: "/api/chat", remoteAddr: "192.0.2.10:5000", host: "relay.example:8787", wantStatus: http.StatusOK},
		{name: "same-origin browser proxy API is allowed", path: "/api/chat", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:8787", origin: "http://127.0.0.1:8787", wantStatus: http.StatusOK, wantCORS: "http://127.0.0.1:8787"},
		{name: "cross-origin browser proxy API is rejected", path: "/api/chat", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:8787", origin: "https://attacker.example", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.RemoteAddr = tt.remoteAddr
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantCORS {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantCORS)
			}
		})
	}
}

func TestManagementPathClassification(t *testing.T) {
	for _, path := range []string{"/api/config", "/api/dashboard", "/api/models"} {
		if !isManagementAPIPath(path) {
			t.Fatalf("%s should be a management path", path)
		}
	}
	for _, path := range []string{"/api/chat", "/api/generate", "/v1/models", "/models", "/healthz"} {
		if isManagementAPIPath(path) {
			t.Fatalf("%s should remain a proxy or health path", path)
		}
	}
	for _, path := range []string{"/", "/assets/app.js", "/favicon.ico"} {
		if !isManagementPagePath(path) {
			t.Fatalf("%s should be a management page path", path)
		}
	}
}
