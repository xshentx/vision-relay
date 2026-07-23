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

func (a *app) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.textConfigForRequest(r)
	out, status, err := a.prepareOpenAIChatWithConfig(r.Context(), cfg, body)
	if err != nil {
		writeError(w, status, err)
		return
	}
	var payload map[string]any
	_ = json.Unmarshal(out, &payload)
	stream, _ := payload["stream"].(bool)
	requestURI := canonicalRequestURI(r.URL.RequestURI())
	forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
	defer releaseStream()
	resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), http.MethodPost, requestURI, out, r.Header)
	if forwardErr == nil && stream {
		keepStreamAfterHeaders()
	}
	if !stream {
		if forwardErr != nil {
			writeError(w, http.StatusBadGateway, forwardErr)
			return
		}
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isEventStreamResponse(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) {
		w.Header().Set(responsesFallbackHeader, "sync-response")
		protocol.WriteChatCompletionStreamFromSyncResponse(w, resp)
		return
	}
	if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
		writeUpstream(w, resp)
		return
	}
	if r.Context().Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		writeError(w, http.StatusBadGateway, r.Context().Err())
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	fallbackBody := synchronousPayload(payload)
	fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), http.MethodPost, requestURI, fallbackBody, synchronousRequestHeaders(r.Header))
	if fallbackErr != nil {
		writeError(w, http.StatusBadGateway, fallbackErr)
		return
	}
	w.Header().Set(responsesFallbackHeader, "sync-retry")
	protocol.WriteChatCompletionStreamFromSyncResponse(w, fallbackResp)
}

func (a *app) processOpenAIChat(ctx context.Context, body []byte, header http.Header, requestURI string) (*http.Response, int, error) {
	cfg := textConfigForClient(a.currentConfig(), textProfileClientOpenCode)
	out, status, err := a.prepareOpenAIChatWithConfig(ctx, cfg, body)
	if err != nil {
		return nil, status, err
	}
	resp, err := a.forwardJSON(ctx, a.textEndpoint(cfg), http.MethodPost, canonicalRequestURI(requestURI), out, header)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return resp, resp.StatusCode, nil
}

func (a *app) prepareOpenAIChat(ctx context.Context, body []byte) ([]byte, int, error) {
	return a.prepareOpenAIChatWithConfig(ctx, a.currentConfig(), body)
}

func (a *app) prepareOpenAIChatWithConfig(ctx context.Context, cfg config, body []byte) ([]byte, int, error) {
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
	return out, http.StatusOK, nil
}

func (a *app) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.textConfigForRequest(r)
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
	stream, _ := payload["stream"].(bool)
	if stream {
		if loggingWriter, ok := w.(*loggingResponseWriter); ok {
			loggingWriter.enableDisconnectDrain()
		}
	}
	forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
	defer releaseStream()

	if normalizeProvider(cfg.TextProvider) == "openai" && normalizeWireAPI(cfg.TextWireAPI) != "responses" {
		chatPayload := protocol.ResponsesPayloadToChatCompletions(payload)
		ensureStreamUsage(chatPayload)
		sanitizeOpenAIChatPayload(chatPayload)
		out, _ := json.Marshal(chatPayload)
		resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), r.Method, "/v1/chat/completions", out, r.Header)
		if forwardErr == nil {
			keepStreamAfterHeaders()
		}
		if !stream {
			if forwardErr != nil {
				writeError(w, http.StatusBadGateway, forwardErr)
				return
			}
			protocol.WriteResponsesFromChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && isEventStreamResponse(resp) {
			protocol.WriteStreamingResponsesFromChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && isSuccessfulResponse(resp) {
			// Some compatible providers ignore stream=true and directly return a
			// completed JSON object. Adapt that response instead of requesting the
			// same model output twice.
			w.Header().Set(responsesFallbackHeader, "sync-response")
			protocol.WriteStreamingResponsesFromSyncChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
			protocol.WriteStreamingResponsesFromChatCompletion(w, resp)
			return
		}
		if r.Context().Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			writeError(w, http.StatusBadGateway, r.Context().Err())
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		fallbackBody := synchronousPayload(chatPayload)
		fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, "/v1/chat/completions", fallbackBody, synchronousRequestHeaders(r.Header))
		if fallbackErr != nil {
			writeError(w, http.StatusBadGateway, fallbackErr)
			return
		}
		w.Header().Set(responsesFallbackHeader, "sync-retry")
		protocol.WriteStreamingResponsesFromSyncChatCompletion(w, fallbackResp)
		return
	}

	out := body
	if changed || model != "" {
		out, _ = json.Marshal(payload)
	}
	requestURI := canonicalRequestURI(r.URL.RequestURI())
	resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), r.Method, requestURI, out, r.Header)
	if forwardErr == nil {
		keepStreamAfterHeaders()
	}
	if !stream {
		if forwardErr != nil {
			writeError(w, http.StatusBadGateway, forwardErr)
			return
		}
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isEventStreamResponse(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) {
		w.Header().Set(responsesFallbackHeader, "sync-response")
		protocol.WriteStreamingResponsesFromSyncResponse(w, resp)
		return
	}
	if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
		writeUpstream(w, resp)
		return
	}
	if r.Context().Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		writeError(w, http.StatusBadGateway, r.Context().Err())
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	fallbackBody := synchronousPayload(payload)
	fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, requestURI, fallbackBody, synchronousRequestHeaders(r.Header))
	if fallbackErr != nil {
		writeError(w, http.StatusBadGateway, fallbackErr)
		return
	}
	w.Header().Set(responsesFallbackHeader, "sync-retry")
	protocol.WriteStreamingResponsesFromSyncResponse(w, fallbackResp)
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
	cfg := a.textConfigForRequest(r)
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
	stream, _ := payload["stream"].(bool)
	if normalizeProvider(cfg.TextProvider) == "openai" {
		chatPayload := protocol.AnthropicPayloadToChatCompletions(payload)
		if model != "" {
			chatPayload["model"] = model
		}
		if stream {
			chatPayload["stream"] = true
			ensureStreamUsage(chatPayload)
		}
		sanitizeOpenAIChatPayload(chatPayload)
		out, _ := json.Marshal(chatPayload)
		forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
		defer releaseStream()
		resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), http.MethodPost, "/v1/chat/completions", out, r.Header)
		if forwardErr == nil && stream {
			keepStreamAfterHeaders()
		}
		if !stream {
			if forwardErr != nil {
				writeError(w, http.StatusBadGateway, forwardErr)
				return
			}
			protocol.WriteAnthropicFromChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && isEventStreamResponse(resp) {
			protocol.WriteAnthropicStreamFromChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && isSuccessfulResponse(resp) {
			w.Header().Set(responsesFallbackHeader, "sync-response")
			protocol.WriteAnthropicStreamFromSyncChatCompletion(w, resp)
			return
		}
		if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
			protocol.WriteAnthropicStreamFromChatCompletion(w, resp)
			return
		}
		if r.Context().Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			writeError(w, http.StatusBadGateway, r.Context().Err())
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		fallbackBody := synchronousPayload(chatPayload)
		fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), http.MethodPost, "/v1/chat/completions", fallbackBody, synchronousRequestHeaders(r.Header))
		if fallbackErr != nil {
			writeError(w, http.StatusBadGateway, fallbackErr)
			return
		}
		w.Header().Set(responsesFallbackHeader, "sync-retry")
		protocol.WriteAnthropicStreamFromSyncChatCompletion(w, fallbackResp)
		return
	}

	out := body
	if changed || model != "" {
		out, _ = json.Marshal(payload)
	}
	requestURI := canonicalRequestURI(r.URL.RequestURI())
	forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
	defer releaseStream()
	resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), r.Method, requestURI, out, r.Header)
	if forwardErr == nil && stream {
		keepStreamAfterHeaders()
	}
	if !stream {
		if forwardErr != nil {
			writeError(w, http.StatusBadGateway, forwardErr)
			return
		}
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isEventStreamResponse(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) {
		w.Header().Set(responsesFallbackHeader, "sync-response")
		protocol.WriteAnthropicStreamFromSyncResponse(w, resp)
		return
	}
	if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
		writeUpstream(w, resp)
		return
	}
	if r.Context().Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		writeError(w, http.StatusBadGateway, r.Context().Err())
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	fallbackBody := synchronousPayload(payload)
	fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, requestURI, fallbackBody, synchronousRequestHeaders(r.Header))
	if fallbackErr != nil {
		writeError(w, http.StatusBadGateway, fallbackErr)
		return
	}
	w.Header().Set(responsesFallbackHeader, "sync-retry")
	protocol.WriteAnthropicStreamFromSyncResponse(w, fallbackResp)
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
	cfg := a.textConfigForRequest(r)
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
	cfg := a.textConfigForRequest(r)
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
	requestURI := geminiRequestURIWithEffectiveModel(cfg, r.URL.RequestURI())
	stream := geminiStreamingRequest(requestURI)
	forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
	defer releaseStream()
	resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), r.Method, requestURI, out, r.Header)
	if forwardErr == nil && stream {
		keepStreamAfterHeaders()
	}
	if !stream {
		if forwardErr != nil {
			writeError(w, http.StatusBadGateway, forwardErr)
			return
		}
		writeUpstream(w, resp)
		return
	}
	sse := geminiSSERequested(requestURI, r.Header)
	if forwardErr == nil && isEventStreamResponse(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) && !sse && responseStartsWithJSONArray(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) {
		w.Header().Set(responsesFallbackHeader, "sync-response")
		protocol.WriteGeminiStreamFromSyncResponse(w, resp, sse)
		return
	}
	if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
		writeUpstream(w, resp)
		return
	}
	if r.Context().Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		writeError(w, http.StatusBadGateway, r.Context().Err())
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	fallbackURI := geminiSynchronousRequestURI(requestURI)
	fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, fallbackURI, out, synchronousRequestHeaders(r.Header))
	if fallbackErr != nil {
		writeError(w, http.StatusBadGateway, fallbackErr)
		return
	}
	w.Header().Set(responsesFallbackHeader, "sync-retry")
	protocol.WriteGeminiStreamFromSyncResponse(w, fallbackResp, sse)
}

func (a *app) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.textConfigForRequest(r)
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
	a.forwardOllamaWithFallback(w, r, cfg, payload, out)
}

func (a *app) handleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := a.textConfigForRequest(r)
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
	a.forwardOllamaWithFallback(w, r, cfg, payload, out)
}

func (a *app) forwardOllamaWithFallback(w http.ResponseWriter, r *http.Request, cfg config, payload map[string]any, out []byte) {
	requestURI := r.URL.RequestURI()
	stream := ollamaStreamRequested(payload)
	forwardContext, keepStreamAfterHeaders, releaseStream := upstreamStreamContext(r.Context(), stream)
	defer releaseStream()
	resp, forwardErr := a.forwardJSON(forwardContext, a.textEndpoint(cfg), r.Method, requestURI, out, r.Header)
	if forwardErr == nil && stream {
		keepStreamAfterHeaders()
	}
	if !stream {
		if forwardErr != nil {
			writeError(w, http.StatusBadGateway, forwardErr)
			return
		}
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isNDJSONResponse(resp) {
		writeUpstream(w, resp)
		return
	}
	if forwardErr == nil && isSuccessfulResponse(resp) {
		w.Header().Set(responsesFallbackHeader, "sync-response")
		protocol.WriteOllamaStreamFromSyncResponse(w, resp)
		return
	}
	if forwardErr == nil && !shouldRetryResponseSynchronously(resp) {
		writeUpstream(w, resp)
		return
	}
	if r.Context().Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		writeError(w, http.StatusBadGateway, r.Context().Err())
		return
	}
	if resp != nil {
		resp.Body.Close()
	}
	fallbackBody := synchronousPayload(payload)
	fallbackResp, fallbackErr := a.forwardJSON(r.Context(), a.textEndpoint(cfg), r.Method, requestURI, fallbackBody, synchronousRequestHeaders(r.Header))
	if fallbackErr != nil {
		writeError(w, http.StatusBadGateway, fallbackErr)
		return
	}
	w.Header().Set(responsesFallbackHeader, "sync-retry")
	protocol.WriteOllamaStreamFromSyncResponse(w, fallbackResp)
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
