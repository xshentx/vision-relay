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

func ResponsesPayloadToChatCompletions(payload map[string]any) map[string]any {
	chat := map[string]any{}
	copyIfPresent(chat, payload, "model")
	copyIfPresent(chat, payload, "temperature")
	copyIfPresent(chat, payload, "top_p")
	copyIfPresent(chat, payload, "presence_penalty")
	copyIfPresent(chat, payload, "frequency_penalty")
	copyIfPresent(chat, payload, "stop")
	copyIfPresent(chat, payload, "stream")
	copyIfPresent(chat, payload, "stream_options")
	if tools, ok := responsesToolsToChatTools(payload["tools"]); ok {
		chat["tools"] = tools
		if toolChoice, ok := responsesToolChoiceToChatToolChoice(payload["tool_choice"]); ok {
			chat["tool_choice"] = toolChoice
		}
		copyIfPresent(chat, payload, "parallel_tool_calls")
	}
	if maxTokens, ok := payload["max_output_tokens"]; ok {
		chat["max_tokens"] = maxTokens
	}
	if maxTokens, ok := payload["max_tokens"]; ok {
		chat["max_tokens"] = maxTokens
	}

	messages := make([]any, 0)
	if instructions := firstString(payload["instructions"]); strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	messages = append(messages, responsesInputToMessages(payload["input"])...)
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": ""})
	}
	chat["messages"] = messages
	return chat
}

func responsesToolsToChatTools(value any) ([]any, bool) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	tools := make([]any, 0, len(raw))
	for _, item := range raw {
		tool, _ := item.(map[string]any)
		if tool == nil || firstString(tool["type"]) != "function" {
			continue
		}
		if fn, ok := tool["function"].(map[string]any); ok {
			tools = append(tools, map[string]any{"type": "function", "function": fn})
			continue
		}
		name := firstString(tool["name"])
		if strings.TrimSpace(name) == "" {
			continue
		}
		fn := map[string]any{"name": name}
		copyIfPresent(fn, tool, "description")
		copyIfPresent(fn, tool, "parameters")
		copyIfPresent(fn, tool, "strict")
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}
	return tools, len(tools) > 0
}

func responsesToolChoiceToChatToolChoice(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return v, true
	case map[string]any:
		if firstString(v["type"]) != "function" {
			return nil, false
		}
		name := firstString(v["name"])
		if strings.TrimSpace(name) == "" {
			return nil, false
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}, true
	default:
		return nil, false
	}
}

func responsesInputToMessages(input any) []any {
	switch value := input.(type) {
	case string:
		return []any{map[string]any{"role": "user", "content": value}}
	case []any:
		messages := make([]any, 0, len(value))
		for _, item := range value {
			switch v := item.(type) {
			case string:
				messages = append(messages, map[string]any{"role": "user", "content": v})
			case map[string]any:
				itemType := firstString(v["type"])
				switch itemType {
				case "function_call":
					callID := firstString(v["call_id"], v["id"])
					name := firstString(v["name"])
					arguments := firstString(v["arguments"], "{}")
					if strings.TrimSpace(arguments) == "" {
						arguments = "{}"
					}
					messages = append(messages, map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []any{
							map[string]any{
								"id":   firstString(callID, "call_vision_relay_1"),
								"type": "function",
								"function": map[string]any{
									"name":      name,
									"arguments": arguments,
								},
							},
						},
					})
					continue
				case "function_call_output":
					messages = append(messages, map[string]any{
						"role":         "tool",
						"tool_call_id": firstString(v["call_id"], v["id"], "call_vision_relay_1"),
						"content":      firstString(v["output"], v["content"]),
					})
					continue
				case "reasoning":
					continue
				}
				role := firstString(v["role"], "user")
				if content, ok := v["content"]; ok {
					messages = append(messages, map[string]any{
						"role":    role,
						"content": responsesContentToChatContent(content),
					})
					continue
				}
				if text := responsesContentToText([]any{v}); strings.TrimSpace(text) != "" {
					messages = append(messages, map[string]any{"role": role, "content": text})
				}
			}
		}
		return messages
	default:
		if input == nil {
			return nil
		}
		b, _ := json.Marshal(input)
		return []any{map[string]any{"role": "user", "content": string(b)}}
	}
}

func responsesContentToText(content any) string {
	return strings.TrimSpace(contentToText(content))
}

func responsesContentToChatContent(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return responsesContentToText(content)
	}
	out := make([]any, 0, len(parts))
	hasImage := false
	textParts := make([]string, 0)
	for _, item := range parts {
		part, _ := item.(map[string]any)
		if part == nil {
			continue
		}
		switch firstString(part["type"]) {
		case "text", "input_text", "output_text":
			if text := firstString(part["text"], part["content"]); strings.TrimSpace(text) != "" {
				out = append(out, map[string]any{"type": "text", "text": text})
				textParts = append(textParts, text)
			}
		case "image_url", "input_image":
			if imagePart, ok := openAIChatImagePart(part); ok {
				out = append(out, imagePart)
				hasImage = true
			}
		case "image", "input_file", "file":
			if imagePart, ok := openAIChatFileImagePart(part); ok {
				out = append(out, imagePart)
				hasImage = true
			}
		}
	}
	if hasImage {
		return out
	}
	return strings.TrimSpace(strings.Join(textParts, "\n"))
}

func openAIChatImagePart(part map[string]any) (map[string]any, bool) {
	if imageURL, ok := part["image_url"].(map[string]any); ok {
		url := firstString(imageURL["url"], imageURL["uri"], imageURL["file_uri"], imageURL["fileUri"])
		if url != "" {
			return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
		}
	}
	if url := firstString(part["image_url"], part["imageUrl"], part["url"]); url != "" {
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
	}
	return openAIChatFileImagePart(part)
}

func openAIChatFileImagePart(part map[string]any) (map[string]any, bool) {
	url := imageFileURL(part)
	if url == "" {
		return nil, false
	}
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
}

func WriteResponsesFromChatCompletion(w http.ResponseWriter, resp *http.Response) {
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
		writeError(w, http.StatusBadGateway, fmt.Errorf("invalid upstream chat completions response: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ChatCompletionToResponses(chat))
}

func WriteStreamingResponsesFromChatCompletion(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(w, resp.Body)
		return
	}
	flusher, _ := w.(http.Flusher)
	responseID := "resp_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	messageID := "msg_" + strings.TrimPrefix(responseID, "resp_")
	model := ""
	created := time.Now().Unix()
	var text strings.Builder
	var usage any
	started := false
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
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if id := firstString(chunk["id"]); id != "" && strings.HasPrefix(id, "resp_") {
			responseID = id
		} else if id != "" {
			responseID = "resp_" + strings.TrimPrefix(id, "chatcmpl-")
		}
		if value := firstString(chunk["model"]); value != "" {
			model = value
		}
		if value := numberAsInt64(chunk["created"]); value != 0 {
			created = value
		}
		if chunkUsage, ok := chunk["usage"]; ok && chunkUsage != nil {
			usage = chatUsageToResponses(chunkUsage)
		}
		if !started {
			writeResponseSSE(w, flusher, map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":         responseID,
					"object":     "response",
					"created_at": created,
					"status":     "in_progress",
					"model":      model,
				},
			})
			writeResponseSSE(w, flusher, map[string]any{
				"type":         "response.output_item.added",
				"output_index": 0,
				"item": map[string]any{
					"id":      messageID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []any{},
				},
			})
			writeResponseSSE(w, flusher, map[string]any{
				"type":          "response.content_part.added",
				"item_id":       messageID,
				"output_index":  0,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        "",
					"annotations": []any{},
				},
			})
			started = true
		}
		delta := chatChunkTextDelta(chunk)
		if delta == "" {
			continue
		}
		text.WriteString(delta)
		writeResponseSSE(w, flusher, map[string]any{
			"type":          "response.output_text.delta",
			"item_id":       messageID,
			"output_index":  0,
			"content_index": 0,
			"delta":         delta,
		})
	}
	finalText := text.String()
	if usage == nil {
		usage = map[string]any{"input_tokens": int64(0), "output_tokens": int64(0), "total_tokens": int64(0)}
	}
	writeResponseSSE(w, flusher, map[string]any{
		"type":          "response.output_text.done",
		"item_id":       messageID,
		"output_index":  0,
		"content_index": 0,
		"text":          finalText,
	})
	writeResponseSSE(w, flusher, map[string]any{
		"type":          "response.content_part.done",
		"item_id":       messageID,
		"output_index":  0,
		"content_index": 0,
		"part": map[string]any{
			"type":        "output_text",
			"text":        finalText,
			"annotations": []any{},
		},
	})
	writeResponseSSE(w, flusher, map[string]any{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]any{
			"id":     messageID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []any{map[string]any{
				"type":        "output_text",
				"text":        finalText,
				"annotations": []any{},
			}},
		},
	})
	writeResponseSSE(w, flusher, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":          responseID,
			"object":      "response",
			"created_at":  created,
			"status":      "completed",
			"model":       model,
			"output_text": finalText,
			"output": []any{map[string]any{
				"id":     messageID,
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []any{map[string]any{
					"type":        "output_text",
					"text":        finalText,
					"annotations": []any{},
				}},
			}},
			"usage": usage,
		},
	})
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func ChatCompletionToResponses(chat map[string]any) map[string]any {
	id := firstString(chat["id"], "resp_"+strconv.FormatInt(time.Now().UnixNano(), 36))
	model := firstString(chat["model"])
	created := numberAsInt64(chat["created"])
	if created == 0 {
		created = time.Now().Unix()
	}
	text := chatCompletionText(chat)
	output := chatCompletionOutput(chat, id, text)
	return map[string]any{
		"id":          responseID(id),
		"object":      "response",
		"created_at":  created,
		"status":      "completed",
		"model":       model,
		"output_text": text,
		"output":      output,
		"usage":       chatUsageToResponses(chat["usage"]),
	}
}

func chatCompletionText(chat map[string]any) string {
	choices, _ := chat["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	return contentToText(message["content"])
}

func chatCompletionOutput(chat map[string]any, id, text string) []any {
	out := make([]any, 0)
	respID := responseID(id)
	if strings.TrimSpace(text) != "" {
		out = append(out, map[string]any{
			"id":     "msg_" + strings.TrimPrefix(respID, "resp_"),
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []any{
				map[string]any{
					"type":        "output_text",
					"text":        text,
					"annotations": []any{},
				},
			},
		})
	}
	choices, _ := chat["choices"].([]any)
	if len(choices) == 0 {
		if len(out) == 0 {
			out = append(out, emptyResponseMessage(respID))
		}
		return out
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	toolCalls, _ := message["tool_calls"].([]any)
	for i, value := range toolCalls {
		call, _ := value.(map[string]any)
		if call == nil {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		callID := firstString(call["id"], "call_vision_relay_"+strconv.Itoa(i+1))
		out = append(out, map[string]any{
			"id":        "fc_" + strings.TrimPrefix(callID, "call_"),
			"type":      "function_call",
			"status":    "completed",
			"call_id":   callID,
			"name":      firstString(fn["name"]),
			"arguments": firstString(fn["arguments"], "{}"),
		})
	}
	if len(out) == 0 {
		out = append(out, emptyResponseMessage(respID))
	}
	return out
}

func emptyResponseMessage(respID string) map[string]any {
	return map[string]any{
		"id":     "msg_" + strings.TrimPrefix(respID, "resp_"),
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []any{
			map[string]any{
				"type":        "output_text",
				"text":        "",
				"annotations": []any{},
			},
		},
	}
}

func chatUsageToResponses(usageValue any) map[string]any {
	usage, _ := usageValue.(map[string]any)
	inputTokens := numberAsInt64(usage["prompt_tokens"])
	outputTokens := numberAsInt64(usage["completion_tokens"])
	totalTokens := numberAsInt64(usage["total_tokens"])
	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}
	out := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  totalTokens,
	}
	if details, _ := usage["prompt_tokens_details"].(map[string]any); details != nil {
		out["input_tokens_details"] = map[string]any{
			"cached_tokens":         firstInt64(details["cached_tokens"], details["cache_read_tokens"]),
			"cache_read_tokens":     firstInt64(details["cache_read_tokens"], details["cached_tokens"]),
			"cache_creation_tokens": firstInt64(details["cache_creation_tokens"], details["cache_write_tokens"]),
		}
	}
	out["cache_read_input_tokens"] = firstInt64(usage["cache_read_input_tokens"], usage["cache_read_tokens"])
	out["cache_creation_input_tokens"] = firstInt64(usage["cache_creation_input_tokens"], usage["cache_creation_tokens"], usage["cache_write_input_tokens"], usage["cache_write_tokens"])
	return out
}

func chatChunkTextDelta(chunk map[string]any) string {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	if delta != nil {
		return contentToText(delta["content"])
	}
	message, _ := choice["message"].(map[string]any)
	if message != nil {
		return contentToText(message["content"])
	}
	return ""
}

func writeResponseSSE(w http.ResponseWriter, flusher http.Flusher, event map[string]any) {
	b, _ := json.Marshal(event)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func responseID(id string) string {
	if strings.HasPrefix(id, "resp_") {
		return id
	}
	return "resp_" + strings.TrimPrefix(id, "chatcmpl-")
}

func numberAsInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}

func copyIfPresent(dst, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}
