package server

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

func withManagementAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isManagementRequest(r) && !managementRequestAllowed(r) {
			writeError(w, http.StatusForbidden, errManagementAccessDenied)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errManagementAccessDenied = errors.New("management interface is only available from the local origin")

func isManagementRequest(r *http.Request) bool {
	if isManagementAPIPath(r.URL.Path) {
		return true
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && isManagementPagePath(r.URL.Path)
}

func isManagementAPIPath(path string) bool {
	if strings.HasPrefix(path, "/api/break-armor/") {
		return true
	}
	switch path {
	case "/api/desktop/activate",
		"/api/config",
		"/api/dashboard",
		"/api/update",
		"/api/update/progress",
		"/api/client/configure",
		"/api/client/routes/apply",
		"/api/client/restore",
		"/api/settings/detect-clients",
		"/api/client/codex/history",
		"/api/break-armor/status",
		"/api/break-armor/preview",
		"/api/break-armor/apply",
		"/api/break-armor/restore",
		"/api/break-armor/sessions",
		"/api/break-armor/session/preview",
		"/api/break-armor/session/patch",
		"/api/break-armor/session/backups",
		"/api/break-armor/session/restore",
		"/api/break-armor/templates",
		"/api/logs",
		"/api/models",
		"/api/model-test":
		return true
	default:
		return false
	}
}

func isManagementPagePath(path string) bool {
	if path == "/" || strings.HasPrefix(path, "/assets/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".css", ".js", ".png", ".jpg", ".jpeg", ".svg", ".ico", ".webp":
		return true
	default:
		return false
	}
}

func managementRequestAllowed(r *http.Request) bool {
	remoteHost, _, ok := splitHostPort(r.RemoteAddr)
	if !ok || !isLoopbackHost(remoteHost) {
		return false
	}
	requestHost, requestPort, ok := splitHostPort(r.Host)
	if !ok || !isLoopbackHost(requestHost) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return sameRequestOrigin(r, requestHost, requestPort, origin)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return sameRequestOrigin(r, requestHost, requestPort, referer)
	}
	return true
}

func sameRequestOrigin(r *http.Request, requestHost, requestPort, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return false
	}
	originHost := u.Hostname()
	if !isLoopbackHost(originHost) || normalizeHost(originHost) != normalizeHost(requestHost) {
		return false
	}
	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	}
	if !strings.EqualFold(u.Scheme, requestScheme) {
		return false
	}
	return effectivePort(requestScheme, requestPort) == effectivePort(strings.ToLower(u.Scheme), u.Port())
}

func splitHostPort(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		return host, port, host != ""
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.String(), "", true
	}
	if !strings.ContainsAny(value, ":/[]") {
		return value, "", true
	}
	return "", "", false
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeHost(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return strings.ToLower(host)
}

func effectivePort(scheme, port string) string {
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}
