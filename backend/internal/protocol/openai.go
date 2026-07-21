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

// WriteChatCompletionStreamFromSyncResponse adapts a completed Chat
// Completions response to OpenAI-compatible SSE for a streaming client.
func WriteChatCompletionStreamFromSyncResponse(w http.ResponseWriter, resp *http.Response) {
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
	WriteChatCompletionStream(w, chat)
}

// WriteChatCompletionStream emits a synchronous Chat Completions object as a
// compact but standards-shaped SSE sequence.
func WriteChatCompletionStream(w http.ResponseWriter, chat map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	id := firstString(chat["id"], "chatcmpl-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	created := firstInt64(chat["created"])
	if created == 0 {
		created = time.Now().Unix()
	}
	model := firstString(chat["model"])
	choices, _ := chat["choices"].([]any)
	if len(choices) == 0 {
		choices = []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": "stop"}}
	}

	for fallbackIndex, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		index := int(numberAsInt64(choice["index"]))
		if _, ok := choice["index"]; !ok {
			index = fallbackIndex
		}
		message, _ := choice["message"].(map[string]any)
		if message == nil {
			message = map[string]any{}
		}
		writeChatCompletionSSE(w, flusher, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{"index": index, "delta": map[string]any{"role": firstString(message["role"], "assistant")}, "finish_reason": nil}},
		})
		if text := contentToText(message["content"]); text != "" {
			writeChatCompletionSSE(w, flusher, map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{map[string]any{"index": index, "delta": map[string]any{"content": text}, "finish_reason": nil}},
			})
		}
		toolCalls, _ := message["tool_calls"].([]any)
		for toolIndex, rawTool := range toolCalls {
			tool, _ := rawTool.(map[string]any)
			if tool == nil {
				continue
			}
			fn, _ := tool["function"].(map[string]any)
			writeChatCompletionSSE(w, flusher, map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{map[string]any{
					"index": index, "finish_reason": nil,
					"delta": map[string]any{"tool_calls": []any{map[string]any{
						"index": toolIndex, "id": firstString(tool["id"], "call_vision_relay_"+strconv.Itoa(toolIndex+1)),
						"type": "function", "function": map[string]any{"name": firstString(fn["name"]), "arguments": firstString(fn["arguments"], "{}")},
					}}},
				}},
			})
		}
		finishReason := firstString(choice["finish_reason"])
		if finishReason == "" {
			if len(toolCalls) > 0 {
				finishReason = "tool_calls"
			} else {
				finishReason = "stop"
			}
		}
		writeChatCompletionSSE(w, flusher, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{"index": index, "delta": map[string]any{}, "finish_reason": finishReason}},
		})
	}
	if usage := chat["usage"]; usage != nil {
		writeChatCompletionSSE(w, flusher, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{}, "usage": usage,
		})
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func writeChatCompletionSSE(w http.ResponseWriter, flusher http.Flusher, chunk map[string]any) {
	body, _ := json.Marshal(chunk)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func IsSSEContentType(value string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), "text/event-stream")
}
