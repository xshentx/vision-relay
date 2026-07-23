package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"time"
	"vision-relay/backend/internal/protocol"
)

type disconnectingResponseWriter struct {
	header http.Header
	cancel context.CancelFunc
	status int
	writes int
}

func (w *disconnectingResponseWriter) Header() http.Header {
	return w.header
}

func (w *disconnectingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *disconnectingResponseWriter) Write([]byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		w.cancel()
	}
	return 0, errors.New("downstream disconnected")
}

func TestEffectiveTextModelMapsCodexAccountAliases(t *testing.T) {
	cfg := config{
		TextModelMappings: []textModelMapping{
			{Name: "GLM 5.2", Model: "z-ai/glm-5.2"},
			{Name: "DeepSeek V4", Model: "deepseek-ai/deepseek-v4-pro"},
			{Name: "Mini", Model: "moonshotai/kimi-k2"},
		},
	}
	tests := map[string]string{
		"gpt-5.5":      "z-ai/glm-5.2",
		"5.5":          "z-ai/glm-5.2",
		"gpt-5.4":      "deepseek-ai/deepseek-v4-pro",
		"GPT-5.4-Mini": "moonshotai/kimi-k2",
	}
	for requested, want := range tests {
		if got := effectiveTextModel(cfg, requested); got != want {
			t.Fatalf("effectiveTextModel(%q) = %q, want %q", requested, got, want)
		}
	}
}

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
	if !strings.Contains(text, "不要再调用 view_image") {
		t.Fatalf("augmented text should prevent image tool loops: %s", text)
	}
}

func TestAugmentOpenAIResponsesCachesVisionDescriptions(t *testing.T) {
	visionCalls := 0
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCalls++
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "cached image description",
					},
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
	newPayload := func() map[string]any {
		return map[string]any{
			"model": "codex-text",
			"input": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_text", "text": "describe"},
						map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="},
					},
				},
			},
		}
	}

	for i := 0; i < 2; i++ {
		payload := newPayload()
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
		if !strings.Contains(text, "cached image description") {
			t.Fatalf("augmented text missing cached description: %s", text)
		}
	}
	if visionCalls != 1 {
		t.Fatalf("expected one vision upstream call, got %d", visionCalls)
	}
	if !a.lastVision.Cached {
		t.Fatalf("second vision debug entry should be cached: %#v", a.lastVision)
	}
}

func TestDescribeImagesCachesEachImageIndependently(t *testing.T) {
	visionCalls := 0
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "description for " + imageURL,
					},
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
	imageA := imageRef{URL: "data:image/png;base64,QQ=="}
	imageB := imageRef{URL: "data:image/png;base64,Qg=="}
	imageC := imageRef{URL: "data:image/png;base64,Qw=="}

	first, err := a.describeImages(context.Background(), cfg, parsedMessage{
		Text:   "describe",
		Images: []imageRef{imageA, imageB},
	})
	if err != nil {
		t.Fatal(err)
	}
	if visionCalls != 2 {
		t.Fatalf("expected two first-pass vision calls, got %d", visionCalls)
	}
	if !strings.Contains(first, "[图片 1 识别结果]") || !strings.Contains(first, "[图片 2 识别结果]") {
		t.Fatalf("multi-image descriptions should be numbered: %s", first)
	}

	second, err := a.describeImages(context.Background(), cfg, parsedMessage{
		Text:   "describe",
		Images: []imageRef{imageA, imageC},
	})
	if err != nil {
		t.Fatal(err)
	}
	if visionCalls != 3 {
		t.Fatalf("expected only the new image to be described, got %d calls", visionCalls)
	}
	if !strings.Contains(second, imageA.URL) || !strings.Contains(second, imageC.URL) {
		t.Fatalf("cached and new image descriptions should both be present: %s", second)
	}
	if a.lastVision.Cached {
		t.Fatalf("mixed cached/new batch should not be marked fully cached: %#v", a.lastVision)
	}
}

func TestRemoveImageViewToolsKeepsOtherTools(t *testing.T) {
	payload := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "name": "view_image"},
			map[string]any{"type": "function", "function": map[string]any{"name": "shell"}},
		},
		"tool_choice": map[string]any{"type": "function", "name": "view_image"},
	}
	removeImageViewTools(payload)
	tools := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected only non-image tools to remain: %#v", payload["tools"])
	}
	if toolName(tools[0].(map[string]any)) != "shell" {
		t.Fatalf("wrong tool remained: %#v", tools[0])
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("image tool choice should be removed: %#v", payload["tool_choice"])
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

func TestProcessOpenAIChatSkipsVisionWhenDisabled(t *testing.T) {
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		msg := messages[0].(map[string]any)
		content := msg["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("image payload should be forwarded unchanged: %#v", content)
		}
		if content[1].(map[string]any)["type"] != "image_url" {
			t.Fatalf("image part should not be converted to text: %#v", content)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer textServer.Close()
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("vision upstream should not be called when disabled")
	}))
	defer visionServer.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider:   "openai",
			TextBaseURL:    textServer.URL,
			VisionProvider: "openai",
			VisionBaseURL:  visionServer.URL,
			VisionAPIKey:   "vision-key",
			VisionModel:    "vision-test",
			VisionEnabled:  boolPtr(false),
		}),
		httpClient: textServer.Client(),
	}
	body := []byte(`{"model":"glm-5.1","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`)
	resp, _, err := a.processOpenAIChat(context.Background(), body, nil, "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestProcessOpenAIChatBypassesVisionWhenSelectedModelSupportsImages(t *testing.T) {
	textServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		msg := messages[0].(map[string]any)
		content := msg["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("image payload should be forwarded unchanged: %#v", content)
		}
		if content[1].(map[string]any)["type"] != "image_url" {
			t.Fatalf("image part should be preserved: %#v", content)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer textServer.Close()
	visionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("vision upstream should not be called when text model supports images")
	}))
	defer visionServer.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  textServer.URL,
			TextModelMappings: []textModelMapping{
				{Name: "glm-5.1", Model: "glm-5.1", SupportsImages: true},
				{Name: "glm-5.1-text", Model: "glm-5.1-text"},
			},
			VisionProvider: "openai",
			VisionBaseURL:  visionServer.URL,
			VisionAPIKey:   "vision-key",
			VisionModel:    "vision-test",
			VisionEnabled:  boolPtr(true),
		}),
		httpClient: textServer.Client(),
	}
	body := []byte(`{"model":"glm-5.1","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`)
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
	chat := protocol.ResponsesPayloadToChatCompletions(payload)
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
				"output":  "G:\\vision-relay",
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "shell"}},
		},
	}
	chat := protocol.ResponsesPayloadToChatCompletions(payload)
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

func TestResponsesPayloadToChatCompletionsConvertsResponsesTools(t *testing.T) {
	payload := map[string]any{
		"model": "z-ai/glm-5.2",
		"input": "hi",
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "shell",
				"description": "Run a shell command",
				"parameters": map[string]any{
					"type": "object",
				},
			},
			map[string]any{
				"type": "web_search_preview",
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"name": "shell",
		},
		"parallel_tool_calls": true,
	}
	chat := protocol.ResponsesPayloadToChatCompletions(payload)
	tools := chat["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "shell" || fn["description"] != "Run a shell command" {
		t.Fatalf("responses tool was not converted: %#v", tools[0])
	}
	choice := chat["tool_choice"].(map[string]any)
	choiceFn := choice["function"].(map[string]any)
	if choice["type"] != "function" || choiceFn["name"] != "shell" {
		t.Fatalf("tool choice was not converted: %#v", choice)
	}
	if chat["parallel_tool_calls"] != true {
		t.Fatalf("parallel tool calls was not preserved: %#v", chat)
	}
}

func TestResponsesPayloadToChatCompletionsPreservesImages(t *testing.T) {
	payload := map[string]any{
		"model": "z-ai/glm-5.2",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "describe"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="},
				},
			},
		},
	}
	chat := protocol.ResponsesPayloadToChatCompletions(payload)
	messages := chat["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected text and image content: %#v", content)
	}
	if content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("image content was not preserved: %#v", content)
	}
}

func TestLocalAPIRoutesEachProtocolThroughItsSelectedClientProfile(t *testing.T) {
	type upstreamCall struct {
		path  string
		model string
	}
	calls := map[string][]upstreamCall{}
	newUpstream := func(name string, response map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			calls[name] = append(calls[name], upstreamCall{path: r.URL.Path, model: firstString(payload["model"])})
			writeJSON(w, http.StatusOK, response)
		}))
	}

	codexUpstream := newUpstream("codex", map[string]any{
		"id": "resp-client-route", "object": "response", "status": "completed", "model": "codex-upstream", "output": []any{},
	})
	defer codexUpstream.Close()
	claudeUpstream := newUpstream("claude", map[string]any{
		"id": "msg-client-route", "type": "message", "role": "assistant", "model": "claude-upstream",
		"content": []any{map[string]any{"type": "text", "text": "ok"}},
	})
	defer claudeUpstream.Close()
	openCodeUpstream := newUpstream("opencode", map[string]any{
		"id": "chat-client-route", "object": "chat.completion", "model": "opencode-upstream",
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
	})
	defer openCodeUpstream.Close()

	localAPIEnabled := true
	cfg := normalizeSeparateModelProfiles(config{
		LocalAPIEnabled:     &localAPIEnabled,
		ActiveTextProfileID: "legacy-global",
		ActiveTextProfileByClient: map[string]string{
			textProfileClientCodex:    "codex-selected",
			textProfileClientClaude:   "claude-selected",
			textProfileClientOpenCode: "opencode-selected",
		},
		TextModelProfiles: []textModelProfile{
			{ID: "legacy-global", Name: "Legacy global", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: "http://127.0.0.1:1", ModelMappings: []textModelMapping{{Name: "client-alias", Model: "wrong-global"}}},
			{ID: "codex-selected", Name: "Codex selected", Client: textProfileClientCodex, Provider: "openai", WireAPI: "responses", BaseURL: codexUpstream.URL, ModelMappings: []textModelMapping{{Name: "client-alias", Model: "codex-upstream"}}},
			{ID: "claude-selected", Name: "Claude selected", Client: textProfileClientClaude, Provider: "anthropic", BaseURL: claudeUpstream.URL, ModelMappings: []textModelMapping{{Name: "client-alias", Model: "claude-upstream"}}},
			{ID: "opencode-selected", Name: "OpenCode selected", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: openCodeUpstream.URL, ModelMappings: []textModelMapping{{Name: "client-alias", Model: "opencode-upstream"}}},
		},
	})
	a := &app{cfg: cfg, httpClient: http.DefaultClient}

	tests := []struct {
		name      string
		path      string
		body      string
		upstream  string
		wantPath  string
		wantModel string
	}{
		{name: "Codex Responses", path: "/v1/responses", body: `{"model":"client-alias","input":"hello"}`, upstream: "codex", wantPath: "/v1/responses", wantModel: "codex-upstream"},
		{name: "Claude Messages", path: "/v1/messages", body: `{"model":"client-alias","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, upstream: "claude", wantPath: "/v1/messages", wantModel: "claude-upstream"},
		{name: "OpenCode Chat", path: "/v1/chat/completions", body: `{"model":"client-alias","messages":[{"role":"user","content":"hello"}]}`, upstream: "opencode", wantPath: "/v1/chat/completions", wantModel: "opencode-upstream"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			a.handleRoute(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			got := calls[test.upstream]
			if len(got) != 1 || got[0].path != test.wantPath || got[0].model != test.wantModel {
				t.Fatalf("selected upstream calls = %#v, want path %q and model %q", got, test.wantPath, test.wantModel)
			}
		})
	}
	if len(calls) != 3 {
		t.Fatalf("requests reached unexpected upstreams: %#v", calls)
	}
	logs := a.currentLogs()
	if len(logs) != 3 || logs[0].UpstreamName != "OpenCode selected" || logs[1].UpstreamName != "Claude selected" || logs[2].UpstreamName != "Codex selected" {
		t.Fatalf("request logs did not use the selected client profiles: %#v", logs)
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

func TestOpenAIResponsesDrainsTerminalUsageAfterDownstreamDisconnect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\n" +
			"data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\"}}\n\n"))
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte("event: response.completed\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-sol\",\"usage\":{\"input_tokens\":100,\"output_tokens\":10,\"total_tokens\":110,\"input_tokens_details\":{\"cached_tokens\":80}}}}\n\n"))
	}))
	defer upstream.Close()

	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			TextWireAPI:  "responses",
		}),
		httpClient: upstream.Client(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &disconnectingResponseWriter{header: make(http.Header), cancel: cancel}
	body := `{"model":"gpt-5.6-sol","stream":true,"input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)).WithContext(ctx)

	a.handleRoute(writer, req)
	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].TotalTokens != 110 || logs[0].CacheHitTokens != 80 {
		t.Fatalf("terminal usage was not persisted after disconnect: %#v", logs)
	}
}

func TestOpenAIChatDrainsTerminalUsageAfterDownstreamDisconnect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-drain\",\"model\":\"gpt-5.6-sol\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-drain\",\"model\":\"gpt-5.6-sol\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":10,\"total_tokens\":110,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n"))
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
	ctx, cancel := context.WithCancel(context.Background())
	writer := &disconnectingResponseWriter{header: make(http.Header), cancel: cancel}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"messages":[{"role":"user","content":"hi"}]}`)).WithContext(ctx)

	a.handleRoute(writer, req)
	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].TotalTokens != 110 || logs[0].CacheHitTokens != 80 {
		t.Fatalf("chat terminal usage was not persisted after disconnect: %#v", logs)
	}
}

func TestUpstreamStreamContextBoundsDrainAfterParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, keepAfterHeaders, release := upstreamStreamContextWithDrainTimeout(parent, true, 20*time.Millisecond)
	defer release()
	keepAfterHeaders()
	cancelParent()

	select {
	case <-ctx.Done():
		t.Fatal("upstream stream was canceled before the drain grace period")
	default:
	}
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("upstream stream was not canceled after the drain grace period")
	}
}

func TestOpenAIResponsesCanUseNativeResponsesUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["input"]; !ok {
			t.Fatalf("responses payload should be forwarded natively: %#v", payload)
		}
		if _, ok := payload["messages"]; ok {
			t.Fatalf("responses payload should not be converted to chat completions: %#v", payload)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "resp-test",
			"object":      "response",
			"status":      "completed",
			"model":       "gpt-5",
			"output_text": "ok",
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			TextWireAPI:  "responses",
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleOpenAIResponses(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"output_text":"ok"`) {
		t.Fatalf("native responses body was not returned: %s", rec.Body.String())
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

func TestAnthropicStreamingRequestStaysStreamingUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if stream, _ := payload["stream"].(bool); !stream {
			t.Fatalf("upstream request should stay streaming: %#v", payload)
		}
		streamOptions, _ := payload["stream_options"].(map[string]any)
		if includeUsage, _ := streamOptions["include_usage"].(bool); !includeUsage {
			t.Fatalf("upstream stream should request usage: %#v", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"model\":\"glm-5.1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\n"))
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
	body := `{"model":"claude-sonnet-4","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	a.handleAnthropicMessages(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", recorder.Code, recorder.Body.String())
	}
	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, "event: content_block_delta") || !strings.Contains(responseBody, `"text":"ok"`) {
		t.Fatalf("anthropic stream was not converted incrementally: %s", responseBody)
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
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(2),
			},
		},
	}
	resp := protocol.ChatCompletionToResponses(chat)
	if resp["object"] != "response" || resp["output_text"] != "ok" {
		t.Fatalf("bad response wrapper: %#v", resp)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != int64(3) || usage["output_tokens"] != int64(4) {
		t.Fatalf("bad usage mapping: %#v", usage)
	}
	details := usage["input_tokens_details"].(map[string]any)
	if details["cached_tokens"] != int64(2) {
		t.Fatalf("bad cache usage mapping: %#v", usage)
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

func TestOpenAIModelsDoesNotAdvertiseImageSupportWhenDisabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			TextProvider:  "openai",
			TextBaseURL:   upstream.URL,
			VisionEnabled: boolPtr(false),
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
	if model["attachment"] == true || model["supports_images"] == true || model["vision"] == true {
		t.Fatalf("model should not advertise image support: %#v", model)
	}
}

func TestOpenAIModelsAdvertisesImageSupportFromVisionCapability(t *testing.T) {
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextModelMappings: []textModelMapping{
				{Name: "native-vision-text", Model: "native-vision-text", SupportsImages: true},
				{Name: "plain-text", Model: "plain-text"},
			},
			VisionEnabled: boolPtr(false),
		}),
		httpClient: http.DefaultClient,
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
	for _, item := range data {
		model := item.(map[string]any)
		imageCapable := model["attachment"] == true && model["supports_images"] == true && model["vision"] == true
		switch model["id"] {
		case "native-vision-text":
			if !imageCapable {
				t.Fatalf("native multimodal model should advertise image support without the vision relay: %#v", model)
			}
		case "plain-text":
			if imageCapable {
				t.Fatalf("plain text model should not advertise image support while the vision relay is disabled: %#v", model)
			}
		}
	}

	a.cfg.VisionEnabled = boolPtr(true)
	rec = httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	payload = nil
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	data = payload["data"].([]any)
	for _, item := range data {
		model := item.(map[string]any)
		if model["attachment"] != true || model["supports_images"] != true || model["vision"] != true {
			t.Fatalf("vision-enabled model should advertise image support: %#v", model)
		}
	}
}

func TestNormalizeTextProfileMigratesLegacyImageSupportToMappings(t *testing.T) {
	cfg := normalizeSeparateModelProfiles(config{
		ActiveTextProfileID: "text-legacy",
		TextModelProfiles: []textModelProfile{{
			ID: "text-legacy",
			ModelMappings: []textModelMapping{
				{Name: "model-a", Model: "model-a"},
				{Name: "model-b", Model: "model-b"},
			},
			SupportsImages: true,
		}},
		VisionEnabled: boolPtr(false),
	})

	profile := cfg.TextModelProfiles[0]
	if profile.SupportsImages || cfg.TextSupportsImages {
		t.Fatalf("legacy provider-level image flags should be consumed: %#v", profile)
	}
	for _, mapping := range profile.ModelMappings {
		if !mapping.SupportsImages {
			t.Fatalf("legacy image support was not migrated to model %q: %#v", mapping.Name, profile.ModelMappings)
		}
	}
}

func TestShouldAugmentImagesUsesSelectedModelCapability(t *testing.T) {
	cfg := config{
		TextModelMappings: []textModelMapping{
			{Name: "vision-model", Model: "upstream-vision", SupportsImages: true},
			{Name: "text-model", Model: "upstream-text"},
		},
		VisionEnabled: boolPtr(true),
	}
	if shouldAugmentImages(cfg, "vision-model") {
		t.Fatal("multimodal model should receive images directly")
	}
	if !shouldAugmentImages(cfg, "text-model") {
		t.Fatal("text-only model should use the configured vision relay")
	}
	if !shouldAugmentImages(cfg, "unmarked-model") {
		t.Fatal("a model without an explicit image-capability mark should use the configured vision relay")
	}
	cfg.VisionEnabled = boolPtr(false)
	if shouldAugmentImages(cfg, "vision-model") || shouldAugmentImages(cfg, "text-model") || shouldAugmentImages(cfg, "unmarked-model") {
		t.Fatal("no model should use the vision relay while it is disabled")
	}
}

func TestOpenAIModelsUsesForcedTextModelWhenConfigured(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{"id": "upstream-model", "object": "model"},
			},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{
			TextProvider:      "openai",
			TextBaseURL:       upstream.URL,
			TextModelOverride: "z-ai/glm-5.2",
		},
		httpClient: upstream.Client(),
	}
	req := httptest.NewRequest(http.MethodGet, "/models", nil)
	rec := httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("upstream model list should not be called when a forced model is configured")
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	data := payload["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected one effective model, got %#v", data)
	}
	model := data[0].(map[string]any)
	if model["id"] != "z-ai/glm-5.2" {
		t.Fatalf("wrong effective model: %#v", model)
	}
	if model["attachment"] != true {
		t.Fatalf("effective model should advertise image support: %#v", model)
	}
}

func TestOpenAIModelsUsesMultipleForcedTextModelsWhenConfigured(t *testing.T) {
	a := &app{
		cfg: config{
			TextProvider:       "openai",
			TextModelOverrides: []string{"model-a", "model-b"},
			VisionEnabled:      boolPtr(true),
		},
		httpClient: http.DefaultClient,
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
	if len(data) != 2 {
		t.Fatalf("expected two effective models, got %#v", data)
	}
	first := data[0].(map[string]any)
	second := data[1].(map[string]any)
	if first["id"] != "model-a" || second["id"] != "model-b" {
		t.Fatalf("wrong effective models: %#v", data)
	}
	if first["attachment"] != true || second["attachment"] != true {
		t.Fatalf("effective models should advertise image support: %#v", data)
	}
}

func TestOpenAIModelsUsesTextModelMappingNames(t *testing.T) {
	a := &app{
		cfg: config{
			TextProvider: "openai",
			TextModelMappings: []textModelMapping{
				{Name: "DeepSeek V4 Flash", Model: "deepseek-v4-flash", ContextWindow: 128000},
			},
			VisionEnabled: boolPtr(true),
		},
		httpClient: http.DefaultClient,
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
	if model["id"] != "DeepSeek V4 Flash" || int(model["context_window"].(float64)) != 128000 {
		t.Fatalf("wrong mapped model payload: %#v", model)
	}
}

func TestOpenAIChatKeepsRequestedModelWhenAllowedByTextProfile(t *testing.T) {
	var upstreamModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		upstreamModel = firstString(payload["model"])
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   upstreamModel,
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{
			TextProvider:       "openai",
			TextBaseURL:        upstream.URL,
			TextModelOverrides: []string{"model-a", "model-b"},
		},
		httpClient: upstream.Client(),
	}
	body := []byte(`{"model":"model-b","messages":[{"role":"user","content":"hello"}]}`)
	resp, status, err := a.processOpenAIChat(context.Background(), body, nil, "/v1/chat/completions")
	if err != nil {
		t.Fatalf("process chat failed: %v", err)
	}
	defer resp.Body.Close()
	if status != http.StatusOK {
		t.Fatalf("bad status %d", status)
	}
	if upstreamModel != "model-b" {
		t.Fatalf("requested model should be kept, got %q", upstreamModel)
	}
}

func TestOpenAIChatMapsDisplayedModelToActualModel(t *testing.T) {
	var upstreamModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		upstreamModel = firstString(payload["model"])
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   upstreamModel,
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer upstream.Close()
	a := &app{
		cfg: config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			TextModelMappings: []textModelMapping{
				{Name: "DeepSeek V4 Flash", Model: "deepseek-v4-flash"},
			},
		},
		httpClient: upstream.Client(),
	}
	body := []byte(`{"model":"DeepSeek V4 Flash","messages":[{"role":"user","content":"hello"}]}`)
	resp, status, err := a.processOpenAIChat(context.Background(), body, nil, "/v1/chat/completions")
	if err != nil {
		t.Fatalf("process chat failed: %v", err)
	}
	defer resp.Body.Close()
	if status != http.StatusOK {
		t.Fatalf("bad status %d", status)
	}
	if upstreamModel != "deepseek-v4-flash" {
		t.Fatalf("displayed model should map to actual model, got %q", upstreamModel)
	}
}

func TestRouteLogsActualForwardedModelAfterProfileSwitch(t *testing.T) {
	var upstreamModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		upstreamModel = firstString(payload["model"])
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-log-model",
			"model":   upstreamModel,
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer upstream.Close()

	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider: "openai",
			TextBaseURL:  upstream.URL,
			TextModelMappings: []textModelMapping{
				{Name: "grok-4.5", Model: "grok-4.5"},
			},
		}),
		httpClient: upstream.Client(),
	}
	body := `{"model":"z-ai/glm-5.2","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-opencode")
	rec := httptest.NewRecorder()
	a.handleRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	if upstreamModel != "grok-4.5" {
		t.Fatalf("request should be forwarded with the current actual model, got %q", upstreamModel)
	}
	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].Model != upstreamModel {
		t.Fatalf("log model should match the actual forwarded model %q: %#v", upstreamModel, logs)
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
	if log.UpstreamName != "当前文本上游" || log.UpstreamProvider != "openai" || log.InputTokens != 11 || log.OutputTokens != 7 || log.CacheHitTokens != 5 {
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

func TestRouteDoesNotRequireClientToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl-no-auth",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
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
	for _, authorization := range []string{"", "Bearer arbitrary-client-value"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`))
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		a.handleRoute(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("local API rejected authorization %q with status %d: %s", authorization, rec.Code, rec.Body.String())
		}
	}
}

func TestFillUsageFromPayloadIncludesSeparateCacheTokensInDerivedTotal(t *testing.T) {
	var log requestLog
	fillUsageFromPayload(&log, map[string]any{
		"usage": map[string]any{
			"input_tokens":                20,
			"output_tokens":               10,
			"cache_read_input_tokens":     80,
			"cache_creation_input_tokens": 5,
		},
	})
	if log.InputTokens != 20 || log.OutputTokens != 10 || log.CacheHitTokens != 80 || log.CacheWriteTokens != 5 || log.TotalTokens != 115 {
		t.Fatalf("separate cache usage was not included in total: %#v", log)
	}
}

func TestFillUsageFromPayloadDoesNotDoubleCountNestedCacheDetails(t *testing.T) {
	var log requestLog
	fillUsageFromPayload(&log, map[string]any{
		"usage": map[string]any{
			"input_tokens":  100,
			"output_tokens": 10,
			"input_tokens_details": map[string]any{
				"cached_tokens": 80,
			},
		},
	})
	if log.InputTokens != 100 || log.OutputTokens != 10 || log.CacheHitTokens != 80 || log.TotalTokens != 110 {
		t.Fatalf("nested cache details were double counted: %#v", log)
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

func TestFillUsageFromSSEDoesNotAccumulateRepeatedUsageSnapshots(t *testing.T) {
	var log requestLog
	body := []byte("data: {\"id\":\"x\",\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n" +
		"data: {\"id\":\"x\",\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 100 || log.OutputTokens != 20 || log.TotalTokens != 120 || log.CacheHitTokens != 80 {
		t.Fatalf("repeated SSE usage was accumulated: %#v", log)
	}
}

func TestFillUsageFromPayloadDoesNotAccumulateAliasedCacheFields(t *testing.T) {
	var log requestLog
	fillUsageFromPayload(&log, map[string]any{
		"response": map[string]any{"usage": map[string]any{
			"input_tokens": 100, "output_tokens": 20, "total_tokens": 120,
			"input_tokens_details": map[string]any{"cached_tokens": 80},
		}},
		"usage": map[string]any{
			"input_tokens": 100, "output_tokens": 20, "total_tokens": 120,
			"cache_read_input_tokens": 80,
		},
	})
	if log.InputTokens != 100 || log.OutputTokens != 20 || log.TotalTokens != 120 || log.CacheHitTokens != 80 {
		t.Fatalf("aliased nested usage was accumulated: %#v", log)
	}
}

func TestFillUsageFromAnthropicSSECombinesMessageUsage(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "anthropic"
	body := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\",\"usage\":{\"input_tokens\":20,\"cache_read_input_tokens\":80,\"cache_creation_input_tokens\":5}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":20,\"output_tokens\":10,\"cache_read_input_tokens\":80,\"cache_creation_input_tokens\":5}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.Model != "claude-test" || log.InputTokens != 20 || log.OutputTokens != 10 || log.CacheHitTokens != 80 || log.CacheWriteTokens != 5 || log.TotalTokens != 115 {
		t.Fatalf("Anthropic stream usage was not combined: %#v", log)
	}
}

func TestFillUsageFromResponsesUsesTerminalUsage(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "openai"
	body := []byte("data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998,\"input_tokens_details\":{\"cached_tokens\":999}}}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"total_tokens\":120,\"input_tokens_details\":{\"cached_tokens\":80}}}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 100 || log.OutputTokens != 20 || log.TotalTokens != 120 || log.CacheHitTokens != 80 {
		t.Fatalf("Responses terminal usage was not authoritative: %#v", log)
	}
}

func TestFillUsageFromAnthropicSSEUsesDeltaInputCorrection(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "anthropic"
	body := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":20,\"output_tokens\":10}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 20 || log.OutputTokens != 10 || log.TotalTokens != 30 {
		t.Fatalf("Anthropic delta input correction was ignored: %#v", log)
	}
}

func TestFillUsageFromAnthropicSSEUsesExplicitZeroDeltaInput(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "anthropic"
	body := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":100}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":0,\"output_tokens\":5}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 0 || log.OutputTokens != 5 || log.CacheHitTokens != 100 || log.TotalTokens != 105 {
		t.Fatalf("Anthropic explicit zero delta input was ignored: %#v", log)
	}
}

func TestFillUsageFromAnthropicSSEAdoptsDeltaCachePairWithFreshInput(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "anthropic"
	body := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100,\"cache_read_input_tokens\":80,\"cache_creation_input_tokens\":5}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":20,\"output_tokens\":10,\"cache_read_input_tokens\":10,\"cache_creation_input_tokens\":2}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 20 || log.OutputTokens != 10 || log.CacheHitTokens != 10 || log.CacheWriteTokens != 2 || log.TotalTokens != 42 {
		t.Fatalf("Anthropic delta cache pair was not adopted with fresh input: %#v", log)
	}
}

func TestFillUsageFromSSEUsesFinalOpenAISnapshot(t *testing.T) {
	var log requestLog
	body := []byte("data: {\"id\":\"x\",\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\n" +
		"data: {\"id\":\"x\",\"usage\":{\"prompt_tokens\":80,\"completion_tokens\":10,\"total_tokens\":90}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 80 || log.OutputTokens != 10 || log.TotalTokens != 90 {
		t.Fatalf("final OpenAI usage snapshot was not authoritative: %#v", log)
	}
}

func TestFillUsageFromGeminiUsageIncludesThoughtTokens(t *testing.T) {
	var log requestLog
	fillUsageFromPayload(&log, map[string]any{
		"usageMetadata": map[string]any{
			"promptTokenCount": 25, "candidatesTokenCount": 10,
			"thoughtsTokenCount": 15, "totalTokenCount": 50,
		},
	})
	if log.InputTokens != 25 || log.OutputTokens != 25 || log.TotalTokens != 50 {
		t.Fatalf("Gemini thought tokens were not included in output: %#v", log)
	}
}

func TestFillUsageFromSSEUsesFinalGeminiSnapshot(t *testing.T) {
	var log requestLog
	body := []byte("data: {\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":5,\"totalTokenCount\":15}}\n\n" +
		"data: {\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":10,\"totalTokenCount\":30}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.InputTokens != 12 || log.OutputTokens != 18 || log.TotalTokens != 30 {
		t.Fatalf("final Gemini usage snapshot was not authoritative: %#v", log)
	}
}

func TestFillUsageFromPayloadPrefersExplicitZeroCacheAlias(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "openai"
	fillUsageFromPayload(&log, map[string]any{"usage": map[string]any{
		"input_tokens": 100, "output_tokens": 10,
		"cache_read_input_tokens": 0,
		"prompt_tokens_details":   map[string]any{"cached_tokens": 80},
	}})
	if log.CacheHitTokens != 0 || log.TotalTokens != 110 {
		t.Fatalf("lower-priority cache alias replaced explicit zero: %#v", log)
	}
}

func TestOpenAIToAnthropicUsageCountsSeparateCacheInLogTotal(t *testing.T) {
	var log requestLog
	log.Protocol = "Anthropic Messages"
	log.UpstreamProvider = "openai"
	fillUsageFromPayload(&log, map[string]any{"usage": map[string]any{
		"input_tokens": 20, "output_tokens": 10,
		"cache_read_input_tokens":     80,
		"cache_creation_input_tokens": 5,
	}})
	if log.TotalTokens != 115 || log.CacheHitTokens != 80 || log.CacheWriteTokens != 5 {
		t.Fatalf("OpenAI-to-Anthropic usage did not preserve separate cache accounting: %#v", log)
	}
}

func TestOpenAISeparateCacheAliasesAreNotAddedToTotal(t *testing.T) {
	var log requestLog
	log.UpstreamProvider = "openai"
	fillUsageFromPayload(&log, map[string]any{"usage": map[string]any{
		"input_tokens": 100, "output_tokens": 20,
		"cache_read_input_tokens": 80,
	}})
	if log.TotalTokens != 120 || log.CacheHitTokens != 80 {
		t.Fatalf("OpenAI cache aliases changed total semantics: %#v", log)
	}
}

func TestFillUsageFromResponsesCompletedSSE(t *testing.T) {
	var log requestLog
	body := []byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"deepseek-ai/deepseek-v4-pro\",\"usage\":{\"input_tokens\":31,\"output_tokens\":9,\"total_tokens\":40,\"input_tokens_details\":{\"cached_tokens\":7}}}}\n\n")
	fillUsageFromSSE(&log, body)
	if log.Model != "deepseek-ai/deepseek-v4-pro" || log.InputTokens != 31 || log.OutputTokens != 9 || log.TotalTokens != 40 || log.CacheHitTokens != 7 {
		t.Fatalf("bad responses SSE usage: %#v", log)
	}
}

func TestInspectSSELogBodySupportsNamedResponsesEvents(t *testing.T) {
	body := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\n")
	state := inspectSSELogBody(body)
	if !state.IsSSE || !state.Completed || state.Failed {
		t.Fatalf("bad SSE state: %#v", state)
	}
}

func TestResponsesStreamFailureRetainsUpstreamHTTPStatus(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	body := []byte("event: error\n" +
		"data: {\"type\":\"error\",\"code\":\"upstream_timeout\",\"message\":\"stream timed out\"}\n\n")
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now())

	logs := a.currentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}
	if logs[0].Status != http.StatusOK || logs[0].Error != "stream timed out" {
		t.Fatalf("stream failure was logged as success: %#v", logs[0])
	}
}

func TestResponsesIncompleteEventPreservesHTTP200(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	body := []byte("event: response.incomplete\n" +
		"data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"usage\":{\"input_tokens\":8,\"output_tokens\":4,\"total_tokens\":12}}}\n\n")

	state := inspectSSELogBody(body)
	if !state.IsSSE || !state.Completed || state.Failed {
		t.Fatalf("response.incomplete was not treated as a successful terminal event: %#v", state)
	}
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now())

	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].Status != http.StatusOK || logs[0].Error != "" || logs[0].TotalTokens != 12 {
		t.Fatalf("response.incomplete was logged incorrectly: %#v", logs)
	}
}

func TestResponsesStreamWithoutTerminalUsageIsNotLogged(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	body := []byte("event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\"}}\n\n")
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now(), 6300)

	logs := a.currentLogs()
	if len(logs) != 0 {
		t.Fatalf("unfinished Responses stream should not be logged: %#v", logs)
	}
}

func TestResponsesCompletedWithoutUsageIsNotLogged(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	body := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"ok\"}}\n\n")
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now())

	logs := a.currentLogs()
	if len(logs) != 0 {
		t.Fatalf("Responses stream without usage should not be logged: %#v", logs)
	}
}

func TestCanceledIncompleteResponsesStreamIsNotLogged(t *testing.T) {
	a := &app{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)).WithContext(ctx)
	body := []byte("event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now())

	logs := a.currentLogs()
	if len(logs) != 0 {
		t.Fatalf("canceled incomplete Responses stream should not be logged: %#v", logs)
	}
}

func TestEmptyUpstreamResponseIsNotLogged(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test"}`))
	a.logCompletedRequest(req, nil, nil, http.StatusOK, time.Now())
	if logs := a.currentLogs(); len(logs) != 0 {
		t.Fatalf("empty upstream response should not be logged: %#v", logs)
	}
}

func TestResponsesNullErrorIsIgnored(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	body := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"error\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n")
	a.logCompletedRequest(req, nil, body, http.StatusOK, time.Now())

	logs := a.currentLogs()
	if len(logs) != 1 || logs[0].Status != http.StatusOK || logs[0].Error != "" || logs[0].TotalTokens != 12 {
		t.Fatalf("null error was treated as a request failure: %#v", logs)
	}
}

func TestLoggingResponseWriterOnlySuppressesErrorsWhenDrainEnabled(t *testing.T) {
	newWriter := func() *loggingResponseWriter {
		return newLoggingResponseWriter(&disconnectingResponseWriter{header: make(http.Header), cancel: func() {}}, time.Now())
	}
	regular := newWriter()
	if _, err := regular.Write([]byte("payload")); err == nil {
		t.Fatal("regular response writer suppressed a downstream error")
	}

	draining := newWriter()
	draining.enableDisconnectDrain()
	if n, err := draining.Write([]byte("payload")); err != nil || n != len("payload") {
		t.Fatalf("draining response writer returned n=%d err=%v", n, err)
	}
}

func TestLoggingResponseWriterKeepsUsageTail(t *testing.T) {
	rec := httptest.NewRecorder()
	lrw := newLoggingResponseWriter(rec, time.Now())
	_, _ = lrw.Write([]byte("data: " + strings.Repeat("x", maxLogBodySize+4096) + "\n\n"))
	_, _ = lrw.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"tail-model\",\"usage\":{\"input_tokens\":13,\"output_tokens\":17,\"total_tokens\":30}}}\n\n"))

	var log requestLog
	fillUsageFromSSE(&log, lrw.logBody())
	if log.Model != "tail-model" || log.InputTokens != 13 || log.OutputTokens != 17 || log.TotalTokens != 30 {
		t.Fatalf("tail usage was not parsed: %#v", log)
	}
}

func TestRequestStreamMode(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		accept string
		want   string
	}{
		{name: "openai stream", path: "/v1/chat/completions", body: `{"stream":true}`, want: "stream"},
		{name: "openai sync", path: "/v1/responses", body: `{"stream":false}`, want: "sync"},
		{name: "openai default sync", path: "/v1/chat/completions", body: `{}`, want: "sync"},
		{name: "gemini stream path", path: "/v1beta/models/gemini:test:streamGenerateContent", body: `{}`, want: "stream"},
		{name: "ollama default stream", path: "/api/chat", body: `{}`, want: "stream"},
		{name: "ollama explicit sync", path: "/api/generate", body: `{"stream":false}`, want: "sync"},
		{name: "sse accept header", path: "/custom", body: `{}`, accept: "text/event-stream", want: "stream"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			if test.accept != "" {
				req.Header.Set("Accept", test.accept)
			}
			if got := requestStreamMode(req, decodeJSONMap([]byte(test.body))); got != test.want {
				t.Fatalf("requestStreamMode() = %q, want %q", got, test.want)
			}
		})
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
	if logs[0].FirstTokenMS != 0 {
		t.Fatalf("error response should not record first token latency: %#v", logs[0])
	}
}

func TestRequestLogSchemaMigratesRequestMode(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE request_logs (id INTEGER PRIMARY KEY, first_token_ms INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO request_logs(id, first_token_ms) VALUES (1, 250), (2, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := ensureRequestLogColumns(db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT request_mode FROM request_logs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var modes []string
	for rows.Next() {
		var mode string
		if err := rows.Scan(&mode); err != nil {
			t.Fatal(err)
		}
		modes = append(modes, mode)
	}
	if len(modes) != 2 || modes[0] != "stream" || modes[1] != "unknown" {
		t.Fatalf("unexpected migrated request modes: %#v", modes)
	}
}

func TestRepairLegacyRequestLogsRemovesIncompleteStreamsAndPreservesFailures(t *testing.T) {
	db, err := openAppDB(filepath.Join(t.TempDir(), appSlug+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := requestLog{
		At: time.Now(), Method: http.MethodPost, Path: "/v1/responses", Protocol: "Responses",
		Status: http.StatusOK, RequestMode: "stream",
	}
	base.Model = "null-error"
	base.TotalTokens = 12
	base.Error = "null"
	if err := insertRequestLogDB(db, base); err != nil {
		t.Fatal(err)
	}
	base.Model = "missing-usage"
	base.TotalTokens = 0
	base.Error = ""
	if err := insertRequestLogDB(db, base); err != nil {
		t.Fatal(err)
	}
	base.Model = "upstream-failure"
	base.Status = http.StatusBadGateway
	base.Error = "stream timed out"
	if err := insertRequestLogDB(db, base); err != nil {
		t.Fatal(err)
	}
	base.Model = "incomplete-stream"
	base.Status = http.StatusBadGateway
	base.Error = "\u5ba2\u6237\u7aef\u5728\u54cd\u5e94\u5b8c\u6210\u524d\u53d6\u6d88\u8bf7\u6c42\uff0cToken \u7528\u91cf\u4e0d\u53ef\u7528"
	if err := insertRequestLogDB(db, base); err != nil {
		t.Fatal(err)
	}

	if err := repairLegacyRequestLogs(db); err != nil {
		t.Fatal(err)
	}
	logs, err := listRequestLogsDB(db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}
	byModel := map[string]requestLog{}
	for _, log := range logs {
		byModel[log.Model] = log
	}
	if log := byModel["null-error"]; log.Status != http.StatusOK || log.Error != "" {
		t.Fatalf("textual null error was not cleared: %#v", log)
	}
	if _, ok := byModel["missing-usage"]; ok {
		t.Fatalf("historical empty usage log was not removed: %#v", byModel["missing-usage"])
	}
	if log := byModel["upstream-failure"]; log.Status != http.StatusBadGateway || log.Error != "stream timed out" {
		t.Fatalf("explicit upstream stream failure was not preserved: %#v", log)
	}
	if _, ok := byModel["incomplete-stream"]; ok {
		t.Fatalf("historical incomplete stream log was not removed: %#v", byModel["incomplete-stream"])
	}
}
func TestDatabaseStoresConfigAndLogsWithoutBodies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), appSlug+".db")
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
		At:               time.Now(),
		Method:           http.MethodPost,
		Path:             "/v1/chat/completions",
		Protocol:         "Chat Completions",
		Model:            "glm-5.1",
		UpstreamName:     "Text Channel A",
		UpstreamProvider: "openai",
		Status:           200,
		FirstTokenMS:     12,
		RequestMode:      "stream",
		InputTokens:      3,
		OutputTokens:     4,
		RequestText:      "secret input",
		ResponseText:     "secret output",
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
	if logs[0].RequestMode != "stream" {
		t.Fatalf("request stream mode was not stored: %#v", logs[0])
	}
	if logs[0].UpstreamName != "Text Channel A" || logs[0].UpstreamProvider != "openai" {
		t.Fatalf("upstream identity was not stored: %#v", logs[0])
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

func TestNormalizeActiveTextProfilesByClientIgnoresUnknownKeys(t *testing.T) {
	profiles := normalizeTextProfiles([]textModelProfile{
		{ID: "opencode-fallback", Client: textProfileClientOpenCode, Provider: "openai"},
		{ID: "unknown-selected", Client: textProfileClientOpenCode, Provider: "openai"},
		{ID: "claude-canonical", Client: textProfileClientClaude, Provider: "anthropic"},
		{ID: "claude-alias", Client: textProfileClientClaude, Provider: "anthropic"},
	})

	active := normalizeActiveTextProfilesByClient(profiles, map[string]string{
		"unexpected-client": "unknown-selected",
		"claude-code":       "claude-alias",
		"claude":            "claude-canonical",
	}, "")

	if got := active[textProfileClientOpenCode]; got != "opencode-fallback" {
		t.Fatalf("unknown client key changed OpenCode selection: got %q, want %q", got, "opencode-fallback")
	}
	if got := active[textProfileClientClaude]; got != "claude-canonical" {
		t.Fatalf("canonical Claude selection did not take precedence over alias: got %q", got)
	}
}
