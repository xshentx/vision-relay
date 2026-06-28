package server

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func writeUpstream(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func contentToText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0)
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if content, ok := m["content"]; ok {
					if text := contentToText(content); text != "" {
						parts = append(parts, text)
						continue
					}
				}
				if partsValue, ok := m["parts"]; ok {
					if text := contentToText(partsValue); text != "" {
						parts = append(parts, text)
						continue
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if content, ok := v["content"]; ok {
			return contentToText(content)
		}
		if parts, ok := v["parts"]; ok {
			return contentToText(parts)
		}
		if message, ok := v["message"]; ok {
			return contentToText(message)
		}
		if response, ok := v["response"].(string); ok {
			return response
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (a *app) authorized(r *http.Request) bool {
	cfg := a.currentConfig()
	keys := make([]string, 0, len(cfg.ClientAPIKeyEntries))
	for _, entry := range cfg.ClientAPIKeyEntries {
		if key := strings.TrimSpace(entry.Key); key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return true
	}
	candidates := audienceKeys(r.Header)
	if key := strings.TrimSpace(r.URL.Query().Get("key")); key != "" {
		candidates = append(candidates, key)
	}
	for _, got := range candidates {
		for _, want := range keys {
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
				return true
			}
		}
	}
	return false
}

func bearer(h http.Header) string {
	value := h.Get("Authorization")
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return strings.TrimSpace(value)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Local-Token, HTTP-Referer, X-Title, Anthropic-Version, Anthropic-Beta")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    "codex_proxy_error",
		},
	})
}
