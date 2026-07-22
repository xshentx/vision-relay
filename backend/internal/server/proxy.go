package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const upstreamStreamDrainTimeout = 15 * time.Second

func upstreamStreamContext(parent context.Context, stream bool) (context.Context, func(), func()) {
	return upstreamStreamContextWithDrainTimeout(parent, stream, upstreamStreamDrainTimeout)
}

func upstreamStreamContextWithDrainTimeout(parent context.Context, stream bool, drainTimeout time.Duration) (context.Context, func(), func()) {
	if !stream {
		return parent, func() {}, func() {}
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	var mu sync.Mutex
	preserve := false
	released := false
	var drainTimer *time.Timer
	startDrainTimer := func() {
		if released || drainTimer != nil {
			return
		}
		if drainTimeout <= 0 {
			cancel()
			return
		}
		drainTimer = time.AfterFunc(drainTimeout, cancel)
	}
	stopParentCancel := context.AfterFunc(parent, func() {
		mu.Lock()
		defer mu.Unlock()
		if preserve {
			startDrainTimer()
			return
		}
		cancel()
	})
	keepAfterHeaders := func() {
		mu.Lock()
		if !released {
			preserve = true
			if parent.Err() != nil {
				startDrainTimer()
			}
		}
		mu.Unlock()
	}
	release := func() {
		mu.Lock()
		if released {
			mu.Unlock()
			return
		}
		released = true
		if drainTimer != nil {
			drainTimer.Stop()
		}
		mu.Unlock()
		stopParentCancel()
		cancel()
	}
	return ctx, keepAfterHeaders, release
}

func (a *app) forwardJSON(ctx context.Context, ep endpoint, method, requestURI string, body []byte, originalHeader http.Header) (*http.Response, error) {
	if method == "" {
		method = http.MethodPost
	}
	resp, err := a.forwardRaw(ctx, ep, method, requestURI, body, originalHeader)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (a *app) forwardRaw(ctx context.Context, ep endpoint, method, requestURI string, body []byte, originalHeader http.Header) (*http.Response, error) {
	if ep.BaseURL == "" {
		ep.BaseURL = defaultBaseURL(ep.Provider)
	}
	target, err := joinTargetURL(ep.BaseURL, requestURI)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, originalHeader)
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	applyProviderAuth(req, ep, originalHeader)
	client, err := a.upstreamHTTPClient(ep.ProxyURL)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (a *app) upstreamHTTPClient(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return a.httpClient, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: 180 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(parsed),
		},
	}, nil
}

func joinTargetURL(baseURL, requestURI string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", errors.New("base url is empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	pathPart := requestURI
	query := ""
	if idx := strings.Index(requestURI, "?"); idx >= 0 {
		pathPart = requestURI[:idx]
		query = requestURI[idx+1:]
	}
	if basePath == "/v1" && strings.HasPrefix(pathPart, "/v1/") {
		pathPart = strings.TrimPrefix(pathPart, "/v1")
	}
	if basePath == "/v1beta" && strings.HasPrefix(pathPart, "/v1beta/") {
		pathPart = strings.TrimPrefix(pathPart, "/v1beta")
	}
	parsed.Path = basePath + pathPart
	parsed.RawQuery = query
	return parsed.String(), nil
}

func copyRequestHeaders(dst, src http.Header) {
	if src == nil {
		return
	}
	blocked := map[string]bool{
		"authorization":       true,
		"accept-encoding":     true,
		"x-api-key":           true,
		"content-length":      true,
		"host":                true,
		"connection":          true,
		"proxy-connection":    true,
		"keep-alive":          true,
		"transfer-encoding":   true,
		"te":                  true,
		"trailer":             true,
		"upgrade":             true,
		"x-local-token":       true,
		"cf-connecting-ip":    true,
		"x-forwarded-for":     true,
		"x-forwarded-host":    true,
		"x-forwarded-proto":   true,
		"x-real-ip":           true,
		"proxy-authorization": true,
	}
	for key, values := range src {
		if blocked[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func applyProviderAuth(req *http.Request, ep endpoint, originalHeader http.Header) {
	provider := normalizeProvider(ep.Provider)
	apiKey := ep.APIKey
	switch provider {
	case "anthropic":
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	case "gemini":
		if apiKey != "" {
			q := req.URL.Query()
			q.Set("key", apiKey)
			req.URL.RawQuery = q.Encode()
		}
	case "ollama":
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	default:
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
}

func audienceKeys(header http.Header) []string {
	keys := make([]string, 0, 2)
	if v := bearer(header); v != "" {
		keys = append(keys, v)
	}
	if v := strings.TrimSpace(header.Get("X-API-Key")); v != "" {
		keys = append(keys, v)
	}
	if v := strings.TrimSpace(header.Get("X-Local-Token")); v != "" {
		keys = append(keys, v)
	}
	return keys
}
