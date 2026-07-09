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
	if !a.authorized(r) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid client api key"))
		a.logCompletedRequest(r, nil, []byte(`{"error":{"message":"invalid client api key"}}`), http.StatusUnauthorized, time.Now())
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
	path := r.URL.Path
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
