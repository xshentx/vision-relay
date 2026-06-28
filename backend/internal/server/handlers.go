package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"codex-proxy/frontend"
)

func (a *app) handleWeb(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	data, err := frontend.FS.ReadFile(strings.TrimPrefix(path, "/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ctype := mime.TypeByExtension(filepath.Ext(path)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (a *app) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.currentConfig())
	case http.MethodPost:
		var cfg config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := a.setConfig(cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": a.currentConfig()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req["model"] = firstString(req["model"], a.currentConfig().TextModelOverride, "local-text-model")
	req["messages"] = []any{
		map[string]any{"role": "user", "content": req["content"]},
	}
	body, _ := json.Marshal(req)
	resp, status, err := a.processOpenAIChat(r.Context(), body, r.Header, "/v1/chat/completions")
	if err != nil {
		writeError(w, status, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp, status, err := a.processOpenAIChat(r.Context(), body, r.Header, r.URL.RequestURI())
	if err != nil {
		writeError(w, status, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) processOpenAIChat(ctx context.Context, body []byte, header http.Header, requestURI string) (*http.Response, int, error) {
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, http.StatusBadRequest, err
	}
	messages, err := decodeMessages(payload["messages"])
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	parsed := make([]parsedMessage, 0, len(messages))
	hasImage := false
	for _, msg := range messages {
		pm := parseOpenAIMessage(msg)
		if len(pm.Images) > 0 {
			hasImage = true
		}
		parsed = append(parsed, pm)
	}
	if hasImage {
		rawMessages, _ := payload["messages"].([]any)
		for i := range parsed {
			if len(parsed[i].Images) == 0 {
				continue
			}
			analysis, err := a.describeImages(ctx, cfg, parsed[i])
			if err != nil {
				return nil, http.StatusBadGateway, err
			}
			if i < len(rawMessages) {
				if msg, ok := rawMessages[i].(map[string]any); ok {
					msg["content"] = buildAugmentedContent(parsed[i].Text, analysis)
				}
			}
		}
	}
	if cfg.TextModelOverride != "" {
		payload["model"] = cfg.TextModelOverride
	}
	ensureStreamUsage(payload)
	sanitizeOpenAIChatPayload(payload)
	out, _ := json.Marshal(payload)
	resp, err := a.forwardJSON(ctx, a.textEndpoint(cfg), http.MethodPost, canonicalRequestURI(requestURI), out, header)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return resp, resp.StatusCode, nil
}

func (a *app) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	changed, err := a.augmentOpenAIResponses(r.Context(), cfg, payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if cfg.TextModelOverride != "" {
		payload["model"] = cfg.TextModelOverride
	}
	if normalizeProvider(cfg.TextProvider) == "openai" {
		chatPayload := responsesPayloadToChatCompletions(payload)
		ensureStreamUsage(chatPayload)
		sanitizeOpenAIChatPayload(chatPayload)
		out, _ := json.Marshal(chatPayload)
		resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, "/v1/chat/completions", out, r.Header)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if stream, _ := payload["stream"].(bool); stream {
			writeStreamingResponsesFromChatCompletion(w, resp)
			return
		}
		writeResponsesFromChatCompletion(w, resp)
		return
	}
	out := body
	if changed || cfg.TextModelOverride != "" {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, canonicalRequestURI(r.URL.RequestURI()), out, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) augmentOpenAIResponses(ctx context.Context, cfg config, payload map[string]any) (bool, error) {
	input, ok := payload["input"]
	if !ok {
		return false, nil
	}
	changed := false
	switch value := input.(type) {
	case []any:
		for i, item := range value {
			msg, ok := item.(map[string]any)
			if !ok {
				continue
			}
			pm := parsedMessage{}
			if content, ok := msg["content"]; ok {
				pm = parseOpenAIContent(content)
			} else {
				pm = parseOpenAIContent([]any{msg})
			}
			if len(pm.Images) == 0 {
				continue
			}
			analysis, err := a.describeImages(ctx, cfg, pm)
			if err != nil {
				return false, err
			}
			content := []any{map[string]any{
				"type": "input_text",
				"text": buildAugmentedContent(pm.Text, analysis),
			}}
			if _, ok := msg["content"]; ok {
				msg["content"] = content
			} else {
				value[i] = map[string]any{
					"type":    firstString(msg["type"], "message"),
					"role":    firstString(msg["role"], "user"),
					"content": content,
				}
			}
			changed = true
		}
	}
	return changed, nil
}

func (a *app) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	messages, _ := payload["messages"].([]any)
	changed := false
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pm := parseAnthropicContent(msg["content"])
		if len(pm.Images) == 0 {
			continue
		}
		analysis, err := a.describeImages(r.Context(), cfg, pm)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		msg["content"] = buildAugmentedContent(pm.Text, analysis)
		changed = true
	}
	if cfg.TextModelOverride != "" {
		payload["model"] = cfg.TextModelOverride
	}
	if normalizeProvider(cfg.TextProvider) == "openai" {
		chatPayload := anthropicPayloadToChatCompletions(payload)
		if cfg.TextModelOverride != "" {
			chatPayload["model"] = cfg.TextModelOverride
		}
		stream, _ := payload["stream"].(bool)
		if stream {
			chatPayload["stream"] = false
		}
		sanitizeOpenAIChatPayload(chatPayload)
		out, _ := json.Marshal(chatPayload)
		resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), http.MethodPost, "/v1/chat/completions", out, r.Header)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if stream {
			writeAnthropicStreamFromChatCompletion(w, resp)
			return
		}
		writeAnthropicFromChatCompletion(w, resp)
		return
	}
	out := body
	if changed || cfg.TextModelOverride != "" {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, canonicalRequestURI(r.URL.RequestURI()), out, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	if normalizeProvider(cfg.TextProvider) != "openai" {
		resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, canonicalRequestURI(r.URL.RequestURI()), body, r.Header)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeUpstream(w, resp)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	text := anthropicSystemText(payload["system"]) + "\n" + contentToText(payload["messages"])
	writeJSON(w, http.StatusOK, map[string]any{
		"input_tokens": estimateTokens(text),
	})
}

func (a *app) handleGeminiGenerate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	contents, _ := payload["contents"].([]any)
	changed := false
	for _, item := range contents {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pm := parseGeminiParts(content["parts"])
		if len(pm.Images) == 0 {
			continue
		}
		analysis, err := a.describeImages(r.Context(), cfg, pm)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		content["parts"] = []any{map[string]any{"text": buildAugmentedContent(pm.Text, analysis)}}
		changed = true
	}
	out := body
	if changed {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, r.URL.RequestURI(), out, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	messages, _ := payload["messages"].([]any)
	changed := false
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pm := parseOllamaMessage(msg)
		if len(pm.Images) == 0 {
			continue
		}
		analysis, err := a.describeImages(r.Context(), cfg, pm)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		msg["content"] = buildAugmentedContent(pm.Text, analysis)
		delete(msg, "images")
		changed = true
	}
	if cfg.TextModelOverride != "" {
		payload["model"] = cfg.TextModelOverride
	}
	out := body
	if changed || cfg.TextModelOverride != "" {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, r.URL.RequestURI(), out, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) handleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	pm := parseOllamaGenerate(payload)
	changed := false
	if len(pm.Images) > 0 {
		analysis, err := a.describeImages(r.Context(), cfg, pm)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		payload["prompt"] = buildAugmentedContent(pm.Text, analysis)
		delete(payload, "images")
		changed = true
	}
	if cfg.TextModelOverride != "" {
		payload["model"] = cfg.TextModelOverride
	}
	out := body
	if changed || cfg.TextModelOverride != "" {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, r.URL.RequestURI(), out, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}

func (a *app) handleRawProxy(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.currentConfig()
	resp, err := a.forwardRaw(r.Context(), a.textEndpoint(cfg), r.Method, r.URL.RequestURI(), body, r.Header)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeUpstream(w, resp)
}
