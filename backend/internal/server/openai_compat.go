package server

import "strings"

func sanitizeOpenAIChatPayload(payload map[string]any) {
	messages, _ := payload["messages"].([]any)
	if len(messages) == 0 {
		return
	}
	pendingToolCallIDs := make([]string, 0)
	seq := 1
	nextID := func() string {
		id := "call_codex_proxy_" + intToBase36(seq)
		seq++
		return id
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := firstString(msg["role"])
		if role == "assistant" {
			toolCalls, _ := msg["tool_calls"].([]any)
			for _, callValue := range toolCalls {
				call, ok := callValue.(map[string]any)
				if !ok {
					continue
				}
				id := strings.TrimSpace(firstString(call["id"]))
				if id == "" {
					id = nextID()
					call["id"] = id
				}
				pendingToolCallIDs = append(pendingToolCallIDs, id)
			}
			continue
		}
		if role != "tool" {
			continue
		}
		if strings.TrimSpace(firstString(msg["tool_call_id"])) != "" {
			continue
		}
		id := ""
		if len(pendingToolCallIDs) > 0 {
			id = pendingToolCallIDs[0]
			pendingToolCallIDs = pendingToolCallIDs[1:]
		}
		if id == "" {
			id = nextID()
		}
		msg["tool_call_id"] = id
	}
}

func intToBase36(n int) string {
	if n <= 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var out [16]byte
	i := len(out)
	for n > 0 {
		i--
		out[i] = digits[n%36]
		n /= 36
	}
	return string(out[i:])
}
