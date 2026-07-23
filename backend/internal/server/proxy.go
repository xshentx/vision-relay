package server

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	group, routed := providerGroupFromContext(ctx)
	if !routed {
		return a.forwardRawOnce(ctx, ep, method, requestURI, body, originalHeader)
	}
	candidate, configured := a.resolveProviderRoute(ctx, ep)
	if !configured {
		return providerGroupUnconfiguredResponse(group), nil
	}
	trace := providerRouteTraceFromContext(ctx)
	trace.set(providerRouteSelection{
		Group: candidate.Group, ProfileID: candidate.ProfileID, Name: candidate.Name,
		Provider: candidate.Endpoint.Provider, Model: candidate.Endpoint.ModelOverride,
	})

	router := a.textProviderRouter()
	candidate, allowed := router.selectCandidate(candidate)
	if !allowed {
		return providerCircuitOpenResponse(), nil
	}
	router.recordSelection(candidate)
	adaptedURI, adaptedBody := adaptProviderAttempt(candidate, requestURI, body)
	resp, err := a.forwardRawOnce(ctx, candidate.Endpoint, method, adaptedURI, adaptedBody, originalHeader)
	if shouldRecordProviderFailure(ctx, resp, err) {
		router.recordFailure(candidate, providerAttemptError(resp, err))
	} else if err == nil && resp != nil {
		if resp.Body == nil || resp.Body == http.NoBody || resp.ContentLength == 0 {
			router.recordSuccess(candidate)
		} else {
			resp.Body = newProviderObservedBody(ctx, resp.Body, router, candidate)
		}
	} else {
		router.releaseHalfOpenProbe(candidate)
	}
	return resp, err
}

func providerGroupUnconfiguredResponse(group providerGroup) *http.Response {
	body := `{"error":{"message":"no model supplier configured for ` + string(group) + `","type":"provider_group_unconfigured"}}`
	return &http.Response{
		StatusCode:    http.StatusServiceUnavailable,
		Status:        "503 Service Unavailable",
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func providerCircuitOpenResponse() *http.Response {
	body := `{"error":{"message":"active provider circuit is open","type":"provider_circuit_open"}}`
	return &http.Response{
		StatusCode:    http.StatusServiceUnavailable,
		Status:        "503 Service Unavailable",
		Header:        http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func (a *app) forwardRawOnce(ctx context.Context, ep endpoint, method, requestURI string, body []byte, originalHeader http.Header) (*http.Response, error) {
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
