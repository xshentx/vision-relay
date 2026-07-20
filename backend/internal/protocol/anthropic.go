package protocol

import (
	"bufio"
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(resp.StatusCode)
		writeAnthropicSSE(w, nil, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": trimBody(body)}})
		return
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		writeAnthropicSyntheticStreamFromChatCompletion(w, resp)
		return
	}
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	type toolState struct {
		blockIndex int
		id         string
		name       string
		pending    strings.Builder
		started    bool
	}
	messageID := "msg_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	model := ""
	started := false
	textStarted := false
	textBlockIndex := 0
	nextBlockIndex := 0
	finishReason := ""
	inputTokens := int64(0)
	outputTokens := int64(0)
	completed := false
	tools := map[int]*toolState{}
	ensureStarted := func() {
		if started {
			return
		}
		writeAnthropicSSE(w, flusher, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": messageID, "type": "message", "role": "assistant", "model": model,
				"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]any{"input_tokens": int64(0), "output_tokens": int64(0)},
			},
		})
		started = true
	}
	writeToolArguments := func(tool *toolState, arguments string) {
		writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
			"type": "content_block_delta", "index": tool.blockIndex,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": arguments},
		})
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStreamEventSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			completed = true
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if upstreamError, _ := chunk["error"].(map[string]any); upstreamError != nil {
			message := firstString(upstreamError["message"], "upstream stream failed")
			writeAnthropicSSE(w, flusher, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": message}})
			return
		}
		if id := firstString(chunk["id"]); id != "" {
			messageID = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
		}
		if value := firstString(chunk["model"]); value != "" {
			model = value
		}
		if usage, ok := chunk["usage"].(map[string]any); ok {
			inputTokens = numberAsInt64(firstAny(usage["prompt_tokens"], usage["input_tokens"]))
			outputTokens = numberAsInt64(firstAny(usage["completion_tokens"], usage["output_tokens"]))
		}
		ensureStarted()
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}
		if reason := firstString(choice["finish_reason"]); reason != "" {
			finishReason = reason
			completed = true
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if text := contentToText(delta["content"]); text != "" {
			if !textStarted {
				textBlockIndex = nextBlockIndex
				nextBlockIndex++
				textStarted = true
				writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
					"type": "content_block_start", "index": textBlockIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
			}
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
				"type": "content_block_delta", "index": textBlockIndex,
				"delta": map[string]any{"type": "text_delta", "text": text},
			})
		}
		toolCalls, _ := delta["tool_calls"].([]any)
		for fallbackIndex, value := range toolCalls {
			call, _ := value.(map[string]any)
			if call == nil {
				continue
			}
			toolIndex := int(numberAsInt64(call["index"]))
			if _, exists := call["index"]; !exists {
				toolIndex = fallbackIndex
			}
			tool := tools[toolIndex]
			if tool == nil {
				tool = &toolState{blockIndex: nextBlockIndex}
				nextBlockIndex++
				tools[toolIndex] = tool
			}
			if id := firstString(call["id"]); id != "" {
				tool.id = id
			}
			fn, _ := call["function"].(map[string]any)
			if name := firstString(fn["name"]); name != "" {
				tool.name = name
			}
			arguments := firstString(fn["arguments"])
			if !tool.started && tool.name != "" {
				if tool.id == "" {
					tool.id = "call_vision_relay_" + strconv.Itoa(toolIndex+1)
				}
				writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
					"type": "content_block_start", "index": tool.blockIndex,
					"content_block": map[string]any{"type": "tool_use", "id": tool.id, "name": tool.name, "input": map[string]any{}},
				})
				tool.started = true
				if pending := tool.pending.String(); pending != "" {
					writeToolArguments(tool, pending)
					tool.pending.Reset()
				}
			}
			if arguments == "" {
				continue
			}
			if tool.started {
				writeToolArguments(tool, arguments)
			} else {
				tool.pending.WriteString(arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		writeAnthropicSSE(w, flusher, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": err.Error()}})
		return
	}
	if !completed {
		writeAnthropicSSE(w, flusher, "error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": "upstream stream ended before completion"}})
		return
	}
	ensureStarted()
	for blockIndex := 0; blockIndex < nextBlockIndex; blockIndex++ {
		if textStarted && textBlockIndex == blockIndex {
			writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
			continue
		}
		for toolIndex, tool := range tools {
			if tool.blockIndex != blockIndex {
				continue
			}
			if !tool.started {
				if tool.id == "" {
					tool.id = "call_vision_relay_" + strconv.Itoa(toolIndex+1)
				}
				writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
					"type": "content_block_start", "index": tool.blockIndex,
					"content_block": map[string]any{"type": "tool_use", "id": tool.id, "name": tool.name, "input": map[string]any{}},
				})
				if pending := tool.pending.String(); pending != "" {
					writeToolArguments(tool, pending)
				}
			}
			writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{"type": "content_block_stop", "index": tool.blockIndex})
		}
	}
	stopReason := "end_turn"
	switch finishReason {
	case "length":
		stopReason = "max_tokens"
	case "tool_calls", "function_call":
		stopReason = "tool_use"
	}
	writeAnthropicSSE(w, flusher, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": outputTokens},
	})
	writeAnthropicSSE(w, flusher, "message_stop", map[string]any{"type": "message_stop"})
}

func writeAnthropicSyntheticStreamFromChatCompletion(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	message := chatCompletionToAnthropic(chat)
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	startedMessage := copyMap(message)
	startedMessage["content"] = []any{}
	startedMessage["stop_reason"] = nil
	startedMessage["usage"] = map[string]any{"input_tokens": int64(0), "output_tokens": int64(0)}
	writeAnthropicSSE(w, flusher, "message_start", map[string]any{"type": "message_start", "message": startedMessage})

	content, _ := message["content"].([]any)
	for index, value := range content {
		block, _ := value.(map[string]any)
		switch firstString(block["type"]) {
		case "text":
			writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{"type": "text_delta", "text": firstString(block["text"])},
			})
		case "tool_use":
			writeAnthropicSSE(w, flusher, "content_block_start", map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}},
			})
			input, _ := json.Marshal(block["input"])
			writeAnthropicSSE(w, flusher, "content_block_delta", map[string]any{
				"type": "content_block_delta", "index": index,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)},
			})
		}
		writeAnthropicSSE(w, flusher, "content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}

	writeAnthropicSSE(w, flusher, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": message["stop_reason"], "stop_sequence": nil},
		"usage": message["usage"],
	})
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
