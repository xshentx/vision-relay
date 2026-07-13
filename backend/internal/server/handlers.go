package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"vision-relay/backend/internal/protocol"

	"vision-relay/frontend"
)

func (a *app) handleWeb(w http.ResponseWriter, r *http.Request) {
	// The desktop client always uses the same localhost URL. Disable caching so
	// a newly installed executable cannot keep showing an older embedded UI.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	data, err := fs.ReadFile(frontend.FS, strings.TrimPrefix(path, "/"))
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
	cfg := a.currentConfig()
	req["model"] = firstString(effectiveTextModel(cfg, firstString(req["model"])), "local-text-model")
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
	requestedModel := firstString(payload["model"])
	hasImage := false
	for _, msg := range messages {
		pm := parseOpenAIMessage(msg)
		if len(pm.Images) > 0 {
			hasImage = true
		}
		parsed = append(parsed, pm)
	}
	imageAugmented := false
	if hasImage && shouldAugmentImages(cfg, requestedModel) {
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
			imageAugmented = true
		}
	}
	if imageAugmented {
		removeImageViewTools(payload)
	}
	model := effectiveTextModel(cfg, requestedModel)
	if model != "" {
		payload["model"] = model
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
	if changed {
		removeImageViewTools(payload)
	}
	model := effectiveTextModel(cfg, firstString(payload["model"]))
	if model != "" {
		payload["model"] = model
	}
	if normalizeProvider(cfg.TextProvider) == "openai" && normalizeWireAPI(cfg.TextWireAPI) != "responses" {
		chatPayload := protocol.ResponsesPayloadToChatCompletions(payload)
		ensureStreamUsage(chatPayload)
		sanitizeOpenAIChatPayload(chatPayload)
		out, _ := json.Marshal(chatPayload)
		resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, "/v1/chat/completions", out, r.Header)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if stream, _ := payload["stream"].(bool); stream {
			protocol.WriteStreamingResponsesFromChatCompletion(w, resp)
			return
		}
		protocol.WriteResponsesFromChatCompletion(w, resp)
		return
	}
	out := body
	if changed || model != "" {
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
	if !shouldAugmentImages(cfg, firstString(payload["model"])) {
		return false, nil
	}
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

func removeImageViewTools(payload map[string]any) {
	rawTools, ok := payload["tools"].([]any)
	if !ok || len(rawTools) == 0 {
		return
	}
	tools := make([]any, 0, len(rawTools))
	removed := map[string]bool{}
	for _, item := range rawTools {
		tool, _ := item.(map[string]any)
		name := toolName(tool)
		if isImageViewToolName(name) {
			removed[name] = true
			continue
		}
		tools = append(tools, item)
	}
	if len(removed) == 0 {
		return
	}
	if len(tools) == 0 {
		delete(payload, "tools")
	} else {
		payload["tools"] = tools
	}
	if choice, _ := payload["tool_choice"].(map[string]any); isImageViewToolName(toolChoiceName(choice)) {
		delete(payload, "tool_choice")
	}
}

func toolName(tool map[string]any) string {
	if tool == nil {
		return ""
	}
	if name := firstString(tool["name"]); name != "" {
		return name
	}
	if fn, _ := tool["function"].(map[string]any); fn != nil {
		return firstString(fn["name"])
	}
	return ""
}

func toolChoiceName(choice map[string]any) string {
	if choice == nil {
		return ""
	}
	if name := firstString(choice["name"]); name != "" {
		return name
	}
	if fn, _ := choice["function"].(map[string]any); fn != nil {
		return firstString(fn["name"])
	}
	return ""
}

func isImageViewToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "view_image", "open_image", "inspect_image", "read_image":
		return true
	default:
		return false
	}
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
	requestedModel := firstString(payload["model"])
	if shouldAugmentImages(cfg, requestedModel) {
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
	}
	model := effectiveTextModel(cfg, requestedModel)
	if model != "" {
		payload["model"] = model
	}
	if normalizeProvider(cfg.TextProvider) == "openai" {
		chatPayload := protocol.AnthropicPayloadToChatCompletions(payload)
		if model != "" {
			chatPayload["model"] = model
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
			protocol.WriteAnthropicStreamFromChatCompletion(w, resp)
			return
		}
		protocol.WriteAnthropicFromChatCompletion(w, resp)
		return
	}
	out := body
	if changed || model != "" {
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
	text := protocol.AnthropicSystemText(payload["system"]) + "\n" + contentToText(payload["messages"])
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
	if shouldAugmentImages(cfg, geminiRequestedModel(r.URL.RequestURI())) {
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
	}
	out := body
	if changed {
		out, _ = json.Marshal(payload)
	}
	resp, err := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, geminiRequestURIWithEffectiveModel(cfg, r.URL.RequestURI()), out, r.Header)
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
	requestedModel := firstString(payload["model"])
	if shouldAugmentImages(cfg, requestedModel) {
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
	}
	model := effectiveTextModel(cfg, requestedModel)
	if model != "" {
		payload["model"] = model
	}
	out := body
	if changed || model != "" {
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
	changed := false
	requestedModel := firstString(payload["model"])
	if shouldAugmentImages(cfg, requestedModel) {
		pm := parseOllamaGenerate(payload)
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
	}
	model := effectiveTextModel(cfg, requestedModel)
	if model != "" {
		payload["model"] = model
	}
	out := body
	if changed || model != "" {
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

func geminiRequestURIWithEffectiveModel(cfg config, requestURI string) string {
	models := textModelOverrides(cfg)
	if len(models) == 0 {
		return requestURI
	}
	prefixes := []string{"/v1beta/models/", "/v1/models/"}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(requestURI, prefix) {
			continue
		}
		suffixStart := len(prefix)
		suffixIndex := strings.Index(requestURI[suffixStart:], ":")
		if suffixIndex < 0 {
			return requestURI
		}
		modelStart := suffixStart
		modelEnd := suffixStart + suffixIndex
		requested := requestURI[modelStart:modelEnd]
		model := effectiveTextModel(cfg, requested)
		if model == "" {
			return requestURI
		}
		return requestURI[:modelStart] + model + requestURI[modelEnd:]
	}
	return requestURI
}

func geminiRequestedModel(requestURI string) string {
	prefixes := []string{"/v1beta/models/", "/v1/models/"}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(requestURI, prefix) {
			continue
		}
		modelStart := len(prefix)
		suffixIndex := strings.Index(requestURI[modelStart:], ":")
		if suffixIndex < 0 {
			return ""
		}
		return requestURI[modelStart : modelStart+suffixIndex]
	}
	return ""
}
