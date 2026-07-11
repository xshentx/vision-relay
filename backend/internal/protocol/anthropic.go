package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func AnthropicPayloadToChatCompletions(payload map[string]any) map[string]any {
	chat := map[string]any{}
	copyIfPresent(chat, payload, "model")
	copyIfPresent(chat, payload, "temperature")
	copyIfPresent(chat, payload, "top_p")
	if maxTokens, ok := payload["max_tokens"]; ok {
		chat["max_tokens"] = maxTokens
	}
	if stop, ok := payload["stop_sequences"]; ok {
		chat["stop"] = stop
	}
	if tools, ok := payload["tools"].([]any); ok && len(tools) > 0 {
		chat["tools"] = anthropicToolsToChatTools(tools)
	}
	if choice, ok := payload["tool_choice"]; ok {
		chat["tool_choice"] = anthropicToolChoiceToChat(choice)
	}
	messages := make([]any, 0)
	if system := AnthropicSystemText(payload["system"]); system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	rawMessages, _ := payload["messages"].([]any)
	messages = append(messages, anthropicMessagesToChat(rawMessages)...)
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": ""})
	}
	chat["messages"] = messages
	return chat
}

func anthropicMessagesToChat(raw []any) []any {
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		msg, _ := item.(map[string]any)
		if msg == nil {
			continue
		}
		role := firstString(msg["role"], "user")
		blocks, ok := msg["content"].([]any)
		if !ok {
			out = append(out, map[string]any{"role": role, "content": contentToText(msg["content"])})
			continue
		}
		textParts := make([]string, 0)
		contentParts := make([]any, 0)
		hasImage := false
		toolCalls := make([]any, 0)
		for _, blockValue := range blocks {
			block, _ := blockValue.(map[string]any)
			if block == nil {
				continue
			}
			switch firstString(block["type"]) {
			case "text":
				if text := firstString(block["text"]); text != "" {
					textParts = append(textParts, text)
					contentParts = append(contentParts, map[string]any{"type": "text", "text": text})
				}
			case "image":
				if imagePart, ok := anthropicImageBlockToChatPart(block); ok {
					contentParts = append(contentParts, imagePart)
					hasImage = true
				}
			case "tool_use":
				input := block["input"]
				if input == nil {
					input = map[string]any{}
				}
				args, _ := json.Marshal(input)
				toolCalls = append(toolCalls, map[string]any{
					"id":   firstString(block["id"], "call_vision_relay_"+strconv.Itoa(len(toolCalls)+1)),
					"type": "function",
					"function": map[string]any{
						"name":      firstString(block["name"]),
						"arguments": string(args),
					},
				})
			case "tool_result":
				out = append(out, map[string]any{
					"role":         "tool",
					"tool_call_id": firstString(block["tool_use_id"], block["id"], "call_vision_relay_1"),
					"content":      contentToText(block["content"]),
				})
			}
		}
		if len(toolCalls) > 0 {
			out = append(out, map[string]any{"role": "assistant", "content": strings.Join(textParts, "\n"), "tool_calls": toolCalls})
			continue
		}
		if hasImage {
			out = append(out, map[string]any{"role": role, "content": contentParts})
			continue
		}
		if len(textParts) > 0 || role != "assistant" {
			out = append(out, map[string]any{"role": role, "content": strings.Join(textParts, "\n")})
		}
	}
	return out
}

func anthropicImageBlockToChatPart(block map[string]any) (map[string]any, bool) {
	source, _ := block["source"].(map[string]any)
	if source == nil {
		return nil, false
	}
	switch firstString(source["type"]) {
	case "base64":
		data := firstString(source["data"])
		if data == "" {
			return nil, false
		}
		mediaType := firstString(source["media_type"], "image/png")
		return map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": "data:" + mediaType + ";base64," + data},
		}, true
	case "url":
		url := firstString(source["url"])
		if url == "" {
			return nil, false
		}
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
	default:
		return nil, false
	}
}

func anthropicToolsToChatTools(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if tool == nil {
			continue
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        firstString(tool["name"]),
				"description": firstString(tool["description"]),
				"parameters":  firstAny(tool["input_schema"], map[string]any{"type": "object", "properties": map[string]any{}}),
			},
		})
	}
	return out
}

func anthropicToolChoiceToChat(value any) any {
	choice, _ := value.(map[string]any)
	if choice == nil {
		return value
	}
	switch firstString(choice["type"]) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": firstString(choice["name"])}}
	default:
		return "auto"
	}
}

func AnthropicSystemText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		return contentToText(v)
	default:
		return ""
	}
}

func WriteAnthropicFromChatCompletion(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, resp.StatusCode, map[string]any{
			"error": map[string]any{
				"message": fmt.Sprintf("upstream chat completions returned %d: %s", resp.StatusCode, trimBody(body)),
				"type":    "upstream_error",
			},
		})
		return
	}
	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, chatCompletionToAnthropic(chat))
}

func WriteAnthropicStreamFromChatCompletion(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		writeAnthropicSSE(w, nil, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": trimBody(body)}})
		return
	}
	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	msg := chatCompletionToAnthropic(chat)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	writeAnthropicSSE(w, flusher, "message_start", map[string]any{"type": "message_start", "message": msg})
	content, _ := msg["content"].([]any)
	for i, blockValue := range content {
		block, _ := blockValue.(map[string]any)
		if block == nil {
			continue
		}
		start := copyMap(block)
		if start["type"] == "text" {
			start["text"] = ""
		}
		if start["type"] == "tool_use" {
			start["input"] = map[string]any{}
		}
		writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": start})
		switch start["type"] {
		case "text":
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": firstString(block["text"])}})
		case "tool_use":
			inputJSON, _ := json.Marshal(block["input"])
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(inputJSON)}})
		}
		writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	writeAnthropicSSE(w, flusher, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": msg["stop_reason"], "stop_sequence": nil}, "usage": msg["usage"]})
	writeAnthropicSSE(w, flusher, "message_stop", map[string]any{"type": "message_stop"})
}

func chatCompletionToAnthropic(chat map[string]any) map[string]any {
	id := firstString(chat["id"], "msg_"+strconv.FormatInt(time.Now().UnixNano(), 36))
	content := chatCompletionToAnthropicContent(chat)
	stopReason := "end_turn"
	if hasAnthropicToolUse(content) {
		stopReason = "tool_use"
	} else if finish := chatFinishReason(chat); finish == "length" {
		stopReason = "max_tokens"
	}
	return map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         firstString(chat["model"]),
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         chatUsageToAnthropic(chat["usage"]),
	}
}

func chatCompletionToAnthropicContent(chat map[string]any) []any {
	choices, _ := chat["choices"].([]any)
	if len(choices) == 0 {
		return []any{map[string]any{"type": "text", "text": ""}}
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	out := make([]any, 0)
	if text := contentToText(message["content"]); strings.TrimSpace(text) != "" {
		out = append(out, map[string]any{"type": "text", "text": text})
	}
	toolCalls, _ := message["tool_calls"].([]any)
	for i, value := range toolCalls {
		call, _ := value.(map[string]any)
		if call == nil {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		input := map[string]any{}
		_ = json.Unmarshal([]byte(firstString(fn["arguments"], "{}")), &input)
		out = append(out, map[string]any{
			"type":  "tool_use",
			"id":    firstString(call["id"], "call_vision_relay_"+strconv.Itoa(i+1)),
			"name":  firstString(fn["name"]),
			"input": input,
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"type": "text", "text": ""})
	}
	return out
}

func chatUsageToAnthropic(usageValue any) map[string]any {
	usage, _ := usageValue.(map[string]any)
	return map[string]any{
		"input_tokens":  numberAsInt64(firstAny(usage["prompt_tokens"], usage["input_tokens"])),
		"output_tokens": numberAsInt64(firstAny(usage["completion_tokens"], usage["output_tokens"])),
	}
}

func chatFinishReason(chat map[string]any) string {
	choices, _ := chat["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	return firstString(choice["finish_reason"])
}

func hasAnthropicToolUse(content []any) bool {
	for _, value := range content {
		block, _ := value.(map[string]any)
		if firstString(block["type"]) == "tool_use" {
			return true
		}
	}
	return false
}

func writeAnthropicSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
