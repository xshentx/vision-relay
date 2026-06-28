package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAugmentOpenAIResponsesConvertsImagesToText(t *testing.T) {
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected vision path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "vision-test" {
			t.Fatalf("unexpected vision model: %v", payload["model"])
		}
		if payload["max_tokens"] != float64(1200) {
			t.Fatalf("vision max_tokens should allow detailed recognition: %#v", payload["max_tokens"])
		}
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		prompt := content[0].(map[string]any)["text"].(string)
		if !strings.Contains(prompt, "不要直接回答该需求") {
			t.Fatalf("vision prompt should tell the vision model not to answer directly: %s", prompt)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "image says hello",
					},
				},
			},
		})
	}))
	defer visionServer.Close()

	a := &app{httpClient: &http.Client{Timeout: 3 * time.Second}}
	cfg := config{
		VisionProvider: "openai",
		VisionBaseURL:  visionServer.URL,
		VisionAPIKey:   "vision-key",
		VisionModel:    "vision-test",
		VisionPrompt:   "extract image facts",
	}
	payload := map[string]any{
		"model": "codex-text",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "summarize"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="},
				},
			},
		},
	}

	changed, err := a.augmentOpenAIResponses(context.Background(), cfg, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected payload to change")
	}
	input := payload["input"].([]any)
	msg := input[0].(map[string]any)
	content := msg["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "summarize") || !strings.Contains(text, "image says hello") {
		t.Fatalf("augmented text missing expected content: %s", text)
	}
}

func TestAugmentOpenAIResponsesConvertsFileImagePartsToText(t *testing.T) {
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{"content": "file image says login page"},
				},
			},
		})
	}))
	defer visionServer.Close()

	a := &app{httpClient: visionServer.Client()}
	cfg := config{
		VisionProvider: "openai",
		VisionBaseURL:  visionServer.URL,
		VisionAPIKey:   "vision-key",
		VisionModel:    "vision-test",
		VisionPrompt:   "extract image facts",
	}
	payload := map[string]any{
		"model": "codex-text",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "这是啥"},
					map[string]any{
						"type":     "file",
						"filename": "密码页.jpg",
						"mime":     "image/jpeg",
						"data":     "aGVsbG8=",
					},
				},
			},
		},
	}

	changed, err := a.augmentOpenAIResponses(context.Background(), cfg, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected file image payload to change")
	}
	input := payload["input"].([]any)
	msg := input[0].(map[string]any)
	content := msg["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "这是啥") || !strings.Contains(text, "file image says login page") {
		t.Fatalf("augmented text missing expected file image content: %s", text)
	}
}

func TestProcessOpenAIChatConvertsFileImagePartsToText(t *testing.T) {
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		msg := messages[0].(map[string]any)
		content := msg["content"].(string)
		if !strings.Contains(content, "file image says login page") {
			t.Fatalf("text upstream did not receive image analysis: %s", content)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer textServer.Close()
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "file image says login page"}}},
		})
	}))
	defer visionServer.Close()
	a := &app{httpClient: &http.Client{Timeout: 3 * time.Second}}
	cfg := config{
		TextProvider:   "openai",
		TextBaseURL:    textServer.URL,
		VisionProvider: "openai",
		VisionBaseURL:  visionServer.URL,
		VisionAPIKey:   "vision-key",
		VisionModel:    "vision-test",
		VisionPrompt:   "extract image facts",
	}
	a.cfg = normalizeSeparateModelProfiles(cfg)
	body := []byte(`{"model":"glm-5.1","messages":[{"role":"user","content":[{"type":"text","text":"这是啥"},{"type":"file","mime":"image/jpeg","filename":"密码页.jpg","data":"aGVsbG8="}]}]}`)
	resp, _, err := a.processOpenAIChat(context.Background(), body, nil, "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestProcessOpenAIChatRepairsToolCallID(t *testing.T) {
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		assistant := messages[1].(map[string]any)
		toolCalls := assistant["tool_calls"].([]any)
		toolCall := toolCalls[0].(map[string]any)
		tool := messages[2].(map[string]any)
		if firstString(toolCall["id"]) == "" {
			t.Fatalf("assistant tool_call id was not repaired: %#v", assistant)
		}
		if tool["tool_call_id"] != toolCall["id"] {
			t.Fatalf("tool_call_id mismatch: tool=%#v assistant=%#v", tool, assistant)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer textServer.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  textServer.URL,
		}),
		httpClient: textServer.Client(),
	}
	body := []byte(`{"model":"glm-5.1","messages":[{"role":"user","content":"run command"},{"role":"assistant","content":"","tool_calls":[{"type":"function","function":{"name":"shell","arguments":"{}"}}]},{"role":"tool","content":"True"}]}`)
	resp, _, err := a.processOpenAIChat(context.Background(), body, nil, "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestResponsesPayloadToChatCompletions(t *testing.T) {
	payload := map[string]any{
		"model":        "z-ai/glm-5.1",
		"instructions": "be brief",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "hello"},
				},
			},
		},
		"max_output_tokens": float64(128),
	}
	chat := responsesPayloadToChatCompletions(payload)
	messages := chat["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected system and user messages, got %d", len(messages))
	}
	if chat["max_tokens"] != float64(128) {
		t.Fatalf("max_output_tokens was not mapped: %#v", chat["max_tokens"])
	}
	user := messages[1].(map[string]any)
	if user["content"] != "hello" {
		t.Fatalf("unexpected user content: %#v", user["content"])
	}
}

func TestResponsesPayloadToChatCompletionsMapsCodexToolHistory(t *testing.T) {
	payload := map[string]any{
		"model":  "z-ai/glm-5.1",
		"stream": true,
		"input": []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_shell_1",
				"name":      "shell",
				"arguments": `{"cmd":"pwd"}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_shell_1",
				"output":  "G:\\codex-proxy",
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "shell"}},
		},
	}
	chat := responsesPayloadToChatCompletions(payload)
	if chat["stream"] != true {
		t.Fatalf("stream flag was not copied: %#v", chat)
	}
	if _, ok := chat["tools"]; !ok {
		t.Fatalf("tools were not copied: %#v", chat)
	}
	messages := chat["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected two tool history messages, got %d", len(messages))
	}
	assistant := messages[0].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	toolCall := toolCalls[0].(map[string]any)
	tool := messages[1].(map[string]any)
	if toolCall["id"] != "call_shell_1" || tool["tool_call_id"] != "call_shell_1" {
		t.Fatalf("tool call history was not mapped: %#v", messages)
	}
}

func TestOpenAIResponsesStreamingIsConvertedForCodexClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["stream"] != true {
			t.Fatalf("stream flag missing from chat payload: %#v", payload)
		}
		options, _ := payload["stream_options"].(map[string]any)
		if options["include_usage"] != true {
			t.Fatalf("include_usage missing: %#v", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"model\":\"glm-5.1\",\"created\":123,\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"model\":\"glm-5.1\",\"created\":123,\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"model\":\"glm-5.1\",\"created\":123,\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"glm-5.1","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleOpenAIResponses(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "response.output_text.delta") || !strings.Contains(out, "response.completed") || !strings.Contains(out, "你好") {
		t.Fatalf("bad responses stream: %s", out)
	}
}

func TestAnthropicMessagesConvertToOpenAICompatibleTextModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["tools"]; !ok {
			t.Fatalf("anthropic tools were not converted: %#v", payload)
		}
		messages := payload["messages"].([]any)
		if messages[0].(map[string]any)["role"] != "system" {
			t.Fatalf("system prompt was not converted: %#v", messages)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-claude-code",
			"model":   "glm-5.1",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 9, "completion_tokens": 2, "total_tokens": 11},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"claude-sonnet-4","system":"be useful","max_tokens":256,"tools":[{"name":"Bash","description":"run shell","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleAnthropicMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "message" || payload["role"] != "assistant" {
		t.Fatalf("bad anthropic response: %#v", payload)
	}
	content := payload["content"].([]any)
	if content[0].(map[string]any)["text"] != "ok" {
		t.Fatalf("bad content: %#v", payload)
	}
}

func TestAnthropicMessagesConvertToolCallsToClaudeCodeToolUse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "chatcmpl-tool",
			"model": "glm-5.1",
			"choices": []any{map[string]any{"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "Bash",
						"arguments": `{"command":"pwd"}`,
					},
				}},
			}}},
			"usage": map[string]any{"prompt_tokens": 9, "completion_tokens": 2, "total_tokens": 11},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"claude-sonnet-4","max_tokens":256,"messages":[{"role":"user","content":"run pwd"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleAnthropicMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["stop_reason"] != "tool_use" {
		t.Fatalf("tool call should become tool_use: %#v", payload)
	}
	content := payload["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_use" || block["name"] != "Bash" {
		t.Fatalf("bad tool use block: %#v", payload)
	}
}

func TestAnthropicCountTokensForClaudeCode(t *testing.T) {
	a := &app{cfg: normalizeSeparateModelProfiles(config{TextProvider: "openai"})}
	body := `{"model":"claude-sonnet-4","system":"be useful","messages":[{"role":"user","content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleAnthropicCountTokens(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if numberAsInt64(payload["input_tokens"]) == 0 {
		t.Fatalf("count_tokens returned zero: %#v", payload)
	}
}

func TestChatCompletionToResponses(t *testing.T) {
	chat := map[string]any{
		"id":      "chatcmpl-test",
		"model":   "z-ai/glm-5.1",
		"created": float64(123),
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "ok",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     float64(3),
			"completion_tokens": float64(4),
			"total_tokens":      float64(7),
		},
	}
	resp := chatCompletionToResponses(chat)
	if resp["object"] != "response" || resp["output_text"] != "ok" {
		t.Fatalf("bad response wrapper: %#v", resp)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != int64(3) || usage["output_tokens"] != int64(4) {
		t.Fatalf("bad usage mapping: %#v", usage)
	}
}

func TestSupportedClientInterfacePaths(t *testing.T) {
	if !isOpenAIResponsesPath("/v1/responses") || !isOpenAIResponsesPath("/responses") {
		t.Fatal("responses paths should be supported")
	}
	if !isOpenAIChatPath("/v1/chat/completions") || !isOpenAIChatPath("/chat/completions") {
		t.Fatal("chat completions paths should be supported")
	}
	if !isOpenAIModelsPath("/v1/models") || !isOpenAIModelsPath("/models") {
		t.Fatal("models paths should be supported")
	}
	if !isAnthropicMessagesPath("/v1/messages") || !isAnthropicMessagesPath("/messages") {
		t.Fatal("anthropic messages paths should be supported")
	}
	if !isGeminiGeneratePath("/v1beta/models/gemini-pro:generateContent") || !isGeminiGeneratePath("/v1/models/gemini-pro:streamGenerateContent") {
		t.Fatal("gemini generate paths should be supported")
	}
	if !isOllamaChatPath("/api/chat") || !isOllamaGeneratePath("/api/generate") {
		t.Fatal("ollama paths should be supported")
	}
}

func TestOpenAIModelsAdvertisesImageSupport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected models path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{"id": "glm-5.1", "object": "model"},
			},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
		},
		httpClient: upstream.Client(),
	}
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	rec := httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	data := payload["data"].([]any)
	model := data[0].(map[string]any)
	if model["attachment"] != true {
		t.Fatalf("model does not advertise attachment support: %#v", model)
	}
	if model["supports_images"] != true || model["vision"] != true {
		t.Fatalf("model does not advertise image support: %#v", model)
	}
	modalities := model["modalities"].(map[string]any)
	input := modalities["input"].([]any)
	if len(input) < 2 || input[1] != "image" {
		t.Fatalf("model does not advertise image input: %#v", model)
	}
	capabilities := model["capabilities"].(map[string]any)
	if capabilities["attachments"] != true || capabilities["vision"] != true {
		t.Fatalf("model capabilities do not advertise images: %#v", model)
	}
}

func TestExtractModelItems(t *testing.T) {
	openAI := extractModelItems("openai", map[string]any{
		"data": []any{
			map[string]any{"id": "gpt-4.1"},
			map[string]any{"id": "gpt-4o-mini"},
		},
	})
	if len(openAI) != 2 || openAI[0].ID != "gpt-4.1" {
		t.Fatalf("bad openai models: %#v", openAI)
	}
	gemini := extractModelItems("gemini", map[string]any{
		"models": []any{
			map[string]any{"name": "models/gemini-1.5-pro", "displayName": "Gemini 1.5 Pro"},
		},
	})
	if len(gemini) != 1 || gemini[0].ID != "gemini-1.5-pro" {
		t.Fatalf("bad gemini models: %#v", gemini)
	}
	ollama := extractModelItems("ollama", map[string]any{
		"models": []any{
			map[string]any{"name": "llama3.2:latest"},
		},
	})
	if len(ollama) != 1 || ollama[0].ID != "llama3.2:latest" {
		t.Fatalf("bad ollama models: %#v", ollama)
	}
}

func TestHandleListModelsOpenAICompatible(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected models path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			t.Fatalf("missing upstream auth: %s", r.Header.Get("Authorization"))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{"id": "model-b"},
				map[string]any{"id": "model-a"},
			},
		})
	}))
	defer upstream.Close()
	a := &app{httpClient: upstream.Client()}
	body, _ := json.Marshal(modelListRequest{
		Provider: "openai",
		BaseURL:  upstream.URL,
		APIKey:   "upstream-key",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/models", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleListModels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Models []modelListItem `json:"models"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 2 || payload.Models[0].ID != "model-a" || payload.Models[1].ID != "model-b" {
		t.Fatalf("bad models response: %#v", payload.Models)
	}
}

func TestRouteAppendsConversationLog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chatcmpl-log",
			"model":  "log-model",
			"object": "chat.completion",
			"choices": []any{
				map[string]any{
					"message": map[string]any{"role": "assistant", "content": "hello back"},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     11,
				"completion_tokens": 7,
				"total_tokens":      18,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 5,
				},
			},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			ClientAPIKeyEntries: []clientAPIKeyEntry{
				{Name: "Test Client", Key: "sk-test-client"},
			},
		},
		httpClient: upstream.Client(),
	}
	body := `{"model":"log-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-test-client")
	rec := httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	logs := a.currentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}
	log := logs[0]
	if log.ClientName != "Test Client" || log.InputTokens != 11 || log.OutputTokens != 7 || log.CacheHitTokens != 5 {
		t.Fatalf("bad log: %#v", log)
	}
	if log.RequestText != "" || log.ResponseText != "" {
		t.Fatalf("conversation text should not be stored: %#v", log)
	}
	rawLog, _ := json.Marshal(log)
	if strings.Contains(string(rawLog), "cache_write_tokens") {
		t.Fatalf("cache write tokens should not be exposed: %s", rawLog)
	}
}

func TestFillUsageFromSSE(t *testing.T) {
	var log requestLog
	body := []byte("data: {\"id\":\"x\",\"model\":\"glm-5.1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: {\"id\":\"x\",\"model\":\"glm-5.1\",\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"total_tokens\":20,\"prompt_tokens_details\":{\"cached_tokens\":5}}}\n\n" +
		"data: [DONE]\n\n")
	fillUsageFromSSE(&log, body)
	if log.Model != "glm-5.1" || log.InputTokens != 12 || log.OutputTokens != 8 || log.TotalTokens != 20 || log.CacheHitTokens != 5 {
		t.Fatalf("bad SSE usage: %#v", log)
	}
}

func TestRouteLogsOnlyStatusForHTMLUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(524)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><title>Timeout</title><body>huge error page</body></html>"))
	}))
	defer upstream.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			ClientAPIKeyEntries: []clientAPIKeyEntry{
				{Name: "Test Client", Key: "sk-test-client"},
			},
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"log-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-test-client")
	rec := httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != 524 {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	logs := a.currentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}
	if logs[0].Error != "HTTP 524" {
		t.Fatalf("html error body should not be stored: %#v", logs[0])
	}
}

func TestDatabaseStoresConfigAndLogsWithoutBodies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex-proxy.db")
	db, err := openAppDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := normalizeSeparateModelProfiles(defaultConfig())
	cfg.Addr = "127.0.0.1:9999"
	if err := saveConfigToDB(db, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := loadConfigFromDB(db)
	if err != nil || !ok {
		t.Fatalf("config not loaded ok=%v err=%v", ok, err)
	}
	if loaded.Addr != "127.0.0.1:9999" {
		t.Fatalf("bad loaded config: %#v", loaded)
	}
	a := &app{db: db}
	a.appendRequestLog(requestLog{
		At:           time.Now(),
		Method:       http.MethodPost,
		Path:         "/v1/chat/completions",
		Protocol:     "Chat Completions",
		Model:        "glm-5.1",
		Status:       200,
		FirstTokenMS: 12,
		InputTokens:  3,
		OutputTokens: 4,
		RequestText:  "secret input",
		ResponseText: "secret output",
	})
	logs := a.currentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}
	if logs[0].RequestText != "" || logs[0].ResponseText != "" {
		t.Fatalf("database log should not store bodies: %#v", logs[0])
	}
	if logs[0].InputTokens != 3 || logs[0].OutputTokens != 4 {
		t.Fatalf("tokens were not stored: %#v", logs[0])
	}
	if logs[0].FirstTokenMS != 12 {
		t.Fatalf("first token latency was not stored: %#v", logs[0])
	}
}

func TestNormalizeSeparateModelProfilesAppliesActiveProfiles(t *testing.T) {
	cfg := defaultConfig()
	cfg.ActiveTextProfileID = "text-b"
	cfg.TextModelProfiles = []textModelProfile{
		{
			ID:            "text-a",
			Name:          "Text A",
			Provider:      "openai",
			BaseURL:       "https://text-a.example",
			ModelOverride: "text-a-model",
		},
		{
			ID:            "text-b",
			Name:          "Text B",
			Provider:      "openai",
			BaseURL:       "https://text-b.example",
			ModelOverride: "text-b-model",
		},
	}
	cfg.ActiveVisionProfileID = "vision-b"
	cfg.VisionModelProfiles = []visionModelProfile{
		{
			ID:       "vision-a",
			Name:     "Vision A",
			Provider: "openai",
			BaseURL:  "https://vision-a.example",
			Model:    "vision-a-model",
		},
		{
			ID:       "vision-b",
			Name:     "Vision B",
			Provider: "openai",
			BaseURL:  "https://vision-b.example",
			Model:    "vision-b-model",
		},
	}
	cfg = normalizeSeparateModelProfiles(cfg)
	if cfg.TextBaseURL != "https://text-b.example" || cfg.TextModelOverride != "text-b-model" {
		t.Fatalf("active text profile was not applied: %#v", cfg)
	}
	if cfg.VisionBaseURL != "https://vision-b.example" || cfg.VisionModel != "vision-b-model" {
		t.Fatalf("active vision profile was not applied: %#v", cfg)
	}
}
