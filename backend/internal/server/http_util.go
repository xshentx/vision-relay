package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const maxRelayRequestBodyBytes int64 = 64 << 20

var errRequestBodyTooLarge = errors.New("request body exceeds 64 MB limit")

type capturedRequestBodyContextKey struct{}

func readBody(r *http.Request) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if captured, ok := r.Context().Value(capturedRequestBodyContextKey{}).([]byte); ok {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return captured, nil
	}
	return readLimitedRequestBody(r)
}

func readLimitedRequestBody(r *http.Request) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	if r.ContentLength > maxRelayRequestBodyBytes {
		return nil, errRequestBodyTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRelayRequestBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxRelayRequestBodyBytes {
		return nil, errRequestBodyTooLarge
	}
	return body, nil
}

func captureRequestBody(r *http.Request) ([]byte, error) {
	body, err := readLimitedRequestBody(r)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	*r = *r.WithContext(contextWithCapturedRequestBody(r, body))
	return body, nil
}

func contextWithCapturedRequestBody(r *http.Request, body []byte) context.Context {
	return context.WithValue(r.Context(), capturedRequestBodyContextKey{}, body)
}

func requestBodyErrorStatus(err error) int {
	if errors.Is(err, errRequestBodyTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func contentToText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
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
		if !isManagementRequest(r) {
			if !browserAPIRequestAllowed(r) {
				writeError(w, http.StatusForbidden, errCrossOriginLocalAPI)
				return
			}
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Local-Token, HTTP-Referer, X-Title, Anthropic-Version, Anthropic-Beta")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errCrossOriginLocalAPI = errors.New("cross-origin local API access is not allowed")

func browserAPIRequestAllowed(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	requestHost, requestPort, ok := splitHostPort(r.Host)
	return ok && sameRequestOrigin(r, requestHost, requestPort, origin)
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
			"type":    "vision_relay_error",
		},
	})
}
