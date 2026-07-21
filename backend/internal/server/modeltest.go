package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxModelTestResponseBytes = 4 << 20

type modelTestRequest struct {
	ProfileID string `json:"profile_id"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
}

type modelTestResult struct {
	OK          bool   `json:"ok"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	WireAPI     string `json:"wire_api"`
	Status      int    `json:"status"`
	DurationMS  int64  `json:"duration_ms"`
	RequestID   string `json:"request_id,omitempty"`
	Output      string `json:"output"`
}

type modelTestSpec struct {
	Endpoint endpoint
	Path     string
	Payload  map[string]any
	WireAPI  string
}

func (a *app) handleModelTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req modelTestRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	profile, ok := findTextModelProfile(a.currentConfig(), strings.TrimSpace(req.ProfileID))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("model provider profile not found"))
		return
	}
	model, err := resolveModelTestModel(profile, req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "hi"
	}
	if len(prompt) > 64<<10 {
		writeError(w, http.StatusBadRequest, errors.New("model test prompt is too large"))
		return
	}
	spec, err := buildModelTestSpec(profile, model, prompt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	body, err := json.Marshal(spec.Payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	started := time.Now()
	resp, err := a.forwardJSON(r.Context(), spec.Endpoint, http.MethodPost, spec.Path, body, http.Header{
		"Accept":       []string{"application/json"},
		"Content-Type": []string{"application/json"},
	})
	durationMS := time.Since(started).Milliseconds()
	if err != nil {
		writeModelTestError(w, http.StatusBadGateway, err, 0, durationMS, "")
		return
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxModelTestResponseBytes))
	requestID := modelTestRequestID(resp.Header)
	if readErr != nil {
		writeModelTestError(w, http.StatusBadGateway, readErr, resp.StatusCode, durationMS, requestID)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("upstream returned %s", resp.Status)
		if detail := modelTestUpstreamError(responseBody); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		writeModelTestError(w, http.StatusBadGateway, err, resp.StatusCode, durationMS, requestID)
		return
	}
	var payload any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		writeModelTestError(w, http.StatusBadGateway, errors.New("upstream returned invalid JSON"), resp.StatusCode, durationMS, requestID)
		return
	}
	output := strings.TrimSpace(modelTestOutput(payload))
	if output == "" {
		output = "请求成功，但响应中没有可显示的文本内容。"
	}
	writeJSON(w, http.StatusOK, modelTestResult{
		OK:          true,
		ProfileID:   profile.ID,
		ProfileName: profile.Name,
		Provider:    normalizeProvider(profile.Provider),
		Model:       model,
		WireAPI:     spec.WireAPI,
		Status:      resp.StatusCode,
		DurationMS:  durationMS,
		RequestID:   requestID,
		Output:      output,
	})
}

func findTextModelProfile(cfg config, id string) (textModelProfile, bool) {
	for _, profile := range cfg.TextModelProfiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return textModelProfile{}, false
}

func resolveModelTestModel(profile textModelProfile, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	models := make([]textModelMapping, 0, len(profile.ModelMappings)+len(profile.ModelOverrides)+1)
	models = append(models, profile.ModelMappings...)
	if len(models) == 0 {
		for _, model := range normalizeModelOverrides(profile.ModelOverrides, profile.ModelOverride) {
			models = append(models, textModelMapping{Name: model, Model: model})
		}
	}
	if len(models) == 0 {
		return "", errors.New("this provider has no configured models to test")
	}
	if requested == "" {
		return firstString(models[0].Model, models[0].Name), nil
	}
	for _, mapping := range models {
		if requested == strings.TrimSpace(mapping.Model) || requested == strings.TrimSpace(mapping.Name) {
			return firstString(mapping.Model, mapping.Name), nil
		}
	}
	return "", errors.New("the selected model is not configured for this provider")
}

func buildModelTestSpec(profile textModelProfile, model, prompt string) (modelTestSpec, error) {
	provider := normalizeProvider(profile.Provider)
	ep := endpoint{
		Provider: provider,
		BaseURL:  strings.TrimSpace(profile.BaseURL),
		APIKey:   strings.TrimSpace(profile.APIKey),
		ProxyURL: strings.TrimSpace(profile.ProxyURL),
	}
	switch provider {
	case "anthropic":
		return modelTestSpec{
			Endpoint: ep,
			Path:     "/v1/messages",
			WireAPI:  "messages",
			Payload: map[string]any{
				"model":      model,
				"max_tokens": 256,
				"stream":     false,
				"messages":   []any{map[string]any{"role": "user", "content": prompt}},
			},
		}, nil
	case "gemini":
		model = strings.TrimPrefix(model, "models/")
		return modelTestSpec{
			Endpoint: ep,
			Path:     "/v1beta/models/" + url.PathEscape(model) + ":generateContent",
			WireAPI:  "generateContent",
			Payload: map[string]any{
				"contents": []any{map[string]any{
					"role":  "user",
					"parts": []any{map[string]any{"text": prompt}},
				}},
			},
		}, nil
	case "ollama":
		return modelTestSpec{
			Endpoint: ep,
			Path:     "/api/chat",
			WireAPI:  "chat",
			Payload: map[string]any{
				"model":    model,
				"stream":   false,
				"messages": []any{map[string]any{"role": "user", "content": prompt}},
			},
		}, nil
	case "openai":
		if normalizeWireAPI(profile.WireAPI) == "responses" {
			return modelTestSpec{
				Endpoint: ep,
				Path:     "/v1/responses",
				WireAPI:  "responses",
				Payload: map[string]any{
					"model":  model,
					"input":  prompt,
					"stream": false,
				},
			}, nil
		}
		return modelTestSpec{
			Endpoint: ep,
			Path:     "/v1/chat/completions",
			WireAPI:  "chat_completions",
			Payload: map[string]any{
				"model":    model,
				"stream":   false,
				"messages": []any{map[string]any{"role": "user", "content": prompt}},
			},
		}, nil
	default:
		return modelTestSpec{}, fmt.Errorf("provider %q does not support model testing", provider)
	}
}

func modelTestOutput(payload any) string {
	root, _ := payload.(map[string]any)
	if root == nil {
		return contentToText(payload)
	}
	if text := strings.TrimSpace(firstString(root["output_text"])); text != "" {
		return text
	}
	if choices, ok := root["choices"].([]any); ok {
		for _, choice := range choices {
			item, _ := choice.(map[string]any)
			if item == nil {
				continue
			}
			if text := strings.TrimSpace(contentToText(firstAny(item["message"], item["delta"], item["text"]))); text != "" {
				return text
			}
		}
	}
	for _, key := range []string{"choices", "output", "content", "candidates", "message", "response"} {
		if text := strings.TrimSpace(contentToText(root[key])); text != "" && text != "null" {
			return text
		}
	}
	return ""
}

func modelTestUpstreamError(body []byte) string {
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		if errValue, ok := payload["error"].(map[string]any); ok {
			if message := firstString(errValue["message"], errValue["detail"], errValue["type"]); message != "" {
				return message
			}
		}
		if message := firstString(payload["message"], payload["detail"], payload["error"]); message != "" {
			return message
		}
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 800 {
		message = message[:800] + "…"
	}
	return message
}

func modelTestRequestID(header http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "openai-request-id", "cf-ray"} {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func writeModelTestError(w http.ResponseWriter, status int, err error, upstreamStatus int, durationMS int64, requestID string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    "model_test_error",
		},
		"upstream_status": upstreamStatus,
		"duration_ms":     durationMS,
		"request_id":      requestID,
	})
}
