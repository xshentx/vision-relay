package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (a *app) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if isStaticRequest(r) {
		a.handleWeb(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/break-armor/") {
		writeError(w, http.StatusNotFound, errors.New("break armor interface not found"))
		return
	}
	if !localAPIEnabled(a.currentConfig()) {
		writeError(w, http.StatusServiceUnavailable, errors.New("local API interface is disabled"))
		return
	}
	started := time.Now()
	body, err := captureRequestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		a.logCompletedRequest(r, nil, []byte(err.Error()), http.StatusBadRequest, started)
		return
	}
	lrw := newLoggingResponseWriter(w, started)
	// Keep consuming streaming upstream responses after a downstream client
	// disconnects so terminal usage events are still captured for every API,
	// not only Responses. This mirrors cc-switch's usage collector behavior.
	if requestStreamMode(r, decodeJSONMap(body)) == "stream" {
		lrw.enableDisconnectDrain()
	}
	path := r.URL.Path
	if group, ok := providerGroupForClient(textProfileClientForRequest(r)); ok {
		r = r.WithContext(withProviderRouteContext(r.Context(), group))
		// Reject an unconfigured text group before model rewriting or image
		// augmentation. Vision configuration is global and independent from text
		// supplier grouping; a request that cannot reach a text supplier must never
		// spend a vision call first.
		if _, configured := a.resolveProviderRoute(r.Context(), a.textEndpoint(a.currentConfig())); !configured {
			writeUpstream(lrw, providerGroupUnconfiguredResponse(group))
			a.logCompletedRequest(r, body, lrw.logBody(), lrw.status, started, lrw.firstTokenMS)
			return
		}
	}
	switch {
	case isOpenAIChatPath(path):
		a.handleOpenAIChat(lrw, r)
	case isOpenAIResponsesPath(path):
		a.handleOpenAIResponses(lrw, r)
	case isOpenAIModelsPath(path):
		a.handleOpenAIModels(lrw, r)
	case isAnthropicCountTokensPath(path):
		a.handleAnthropicCountTokens(lrw, r)
	case isAnthropicMessagesPath(path):
		a.handleAnthropicMessages(lrw, r)
	case isGeminiGeneratePath(path):
		a.handleGeminiGenerate(lrw, r)
	case isOllamaChatPath(path):
		a.handleOllamaChat(lrw, r)
	case isOllamaGeneratePath(path):
		a.handleOllamaGenerate(lrw, r)
	default:
		a.handleRawProxy(lrw, r)
	}
	a.logCompletedRequest(r, body, lrw.logBody(), lrw.status, started, lrw.firstTokenMS)
}

func isStaticRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	if path == "/" || strings.HasPrefix(path, "/assets/") {
		return true
	}
	switch filepath.Ext(path) {
	case ".html", ".css", ".js", ".png", ".jpg", ".jpeg", ".svg", ".ico", ".webp":
		return true
	default:
		return false
	}
}

func isOpenAIChatPath(path string) bool {
	return path == "/v1/chat/completions" || path == "/chat/completions"
}

func isOpenAIResponsesPath(path string) bool {
	return path == "/v1/responses" || path == "/responses"
}

func isOpenAIModelsPath(path string) bool {
	return path == "/v1/models" || path == "/models"
}

func isAnthropicMessagesPath(path string) bool {
	return path == "/v1/messages" || path == "/messages"
}

func isAnthropicCountTokensPath(path string) bool {
	return path == "/v1/messages/count_tokens" || path == "/messages/count_tokens"
}

func isGeminiGeneratePath(path string) bool {
	return (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")) &&
		(strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
}

func isOllamaChatPath(path string) bool {
	return path == "/api/chat"
}

func isOllamaGeneratePath(path string) bool {
	return path == "/api/generate"
}

func textProfileClientForRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	path := r.URL.Path
	switch {
	case isOpenAIResponsesPath(path):
		return textProfileClientCodex
	case isAnthropicMessagesPath(path), isAnthropicCountTokensPath(path):
		return textProfileClientClaude
	case isOpenAIChatPath(path), isGeminiGeneratePath(path), isOllamaChatPath(path), isOllamaGeneratePath(path):
		return textProfileClientOpenCode
	default:
		// /models and raw proxy paths cannot reliably identify the caller.
		// Keep the legacy globally active profile for backward compatibility.
		return ""
	}
}

func (a *app) textConfigForRequest(r *http.Request) config {
	cfg := a.currentConfig()
	if r == nil {
		return cfg
	}
	if route := providerRouteRequestFromContext(r.Context()); route != nil {
		candidate, configured := a.resolveProviderRoute(r.Context(), a.textEndpoint(cfg))
		if configured {
			return candidate.Config
		}
		return cfg
	}
	client := textProfileClientForRequest(r)
	if client == "" {
		return cfg
	}
	return textConfigForClient(cfg, client)
}

func canonicalRequestURI(requestURI string) string {
	path := requestURI
	query := ""
	if idx := strings.Index(requestURI, "?"); idx >= 0 {
		path = requestURI[:idx]
		query = requestURI[idx:]
	}
	switch path {
	case "/chat/completions":
		path = "/v1/chat/completions"
	case "/responses":
		path = "/v1/responses"
	case "/messages":
		path = "/v1/messages"
	case "/messages/count_tokens":
		path = "/v1/messages/count_tokens"
	case "/models":
		path = "/v1/models"
	}
	return path + query
}
