package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	managementTokenFileName = "management.token"
	managementTokenQuery    = "management_token"
	managementTokenCookie   = "vision_relay_management"
)

func generateAccessToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate access token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func appUserConfigDir() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, appSlug), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errors.New("locate user configuration directory")
	}
	return filepath.Join(home, ".config", appSlug), nil
}

func defaultManagementTokenPath() (string, error) {
	dir, err := appUserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, managementTokenFileName), nil
}

func loadOrCreateManagementToken(path string) (string, error) {
	if path == "" {
		var err error
		path, err = defaultManagementTokenPath()
		if err != nil {
			return "", err
		}
	}
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if !validAccessToken(token) {
			return "", errors.New("management token file is invalid")
		}
		_ = os.Chmod(path, 0o600)
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read management token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create management token directory: %w", err)
	}
	for attempts := 0; attempts < 3; attempts++ {
		token, err := generateAccessToken()
		if err != nil {
			return "", err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			existing := strings.TrimSpace(string(data))
			if validAccessToken(existing) {
				_ = os.Chmod(path, 0o600)
				return existing, nil
			}
			return "", errors.New("management token file is invalid")
		}
		if err != nil {
			return "", fmt.Errorf("create management token: %w", err)
		}
		if _, err := file.WriteString(token + "\n"); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", fmt.Errorf("write management token: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("close management token: %w", err)
		}
		return token, nil
	}
	return "", errors.New("management token could not be initialized")
}

func validAccessToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}

func accessTokenEqual(expected, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	return expected != "" && len(expected) == len(actual) && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func requestBearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func managementRequestToken(r *http.Request) string {
	if token := requestBearerToken(r); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.Header.Get("X-Vision-Relay-Management-Token")); token != "" {
		return token
	}
	if cookie, err := r.Cookie(managementTokenCookie); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func managementBootstrapURL(rawURL, token string) string {
	u, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(token) == "" {
		return rawURL
	}
	query := u.Query()
	query.Set(managementTokenQuery, token)
	u.RawQuery = query.Encode()
	return u.String()
}

func relayRequiresAuthentication(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return true
	}
	return !isLoopbackHost(host)
}

func ensureRelayToken(cfg *config) (bool, error) {
	if cfg == nil || !relayRequiresAuthentication(cfg.Addr) || strings.TrimSpace(cfg.RelayToken) != "" {
		return false, nil
	}
	token, err := generateAccessToken()
	if err != nil {
		return false, err
	}
	cfg.RelayToken = token
	return true, nil
}

func relayRequestAuthorized(r *http.Request, token string) bool {
	for _, candidate := range []string{
		requestBearerToken(r),
		strings.TrimSpace(r.Header.Get("X-API-Key")),
		strings.TrimSpace(r.Header.Get("X-Local-Token")),
	} {
		if accessTokenEqual(token, candidate) {
			return true
		}
	}
	return false
}

func withRelayAccess(a *app, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		cfg := a.currentConfig()
		// The listener address only changes after restart. Keep the authentication
		// boundary tied to the socket that is actually serving this request rather
		// than a newly saved address that has not taken effect yet.
		if a.relayAuthRequired && !relayRequestAuthorized(r, cfg.RelayToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="vision-relay"`)
			writeError(w, http.StatusUnauthorized, errors.New("relay authentication required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
