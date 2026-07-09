package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxVisionCacheEntries = 128

func (a *app) describeImages(ctx context.Context, cfg config, pm parsedMessage) (string, error) {
	if len(pm.Images) == 0 {
		return "", nil
	}
	ep := a.visionEndpoint(cfg)
	if ep.APIKey == "" && ep.Provider != "ollama" {
		err := errors.New("vision api key is empty")
		a.recordVisionDebug(ep, pm, "", false, err)
		return "", err
	}
	if len(pm.Images) == 1 {
		text, cached, err := a.describeOneImage(ctx, cfg, ep, pm)
		a.recordVisionDebug(ep, pm, text, cached, err)
		return text, err
	}

	parts := make([]string, 0, len(pm.Images))
	allCached := true
	for i, img := range pm.Images {
		single := pm
		single.Images = []imageRef{img}
		text, cached, err := a.describeOneImage(ctx, cfg, ep, single)
		if err != nil {
			err = fmt.Errorf("image %d: %w", i+1, err)
			a.recordVisionDebug(ep, pm, strings.Join(parts, "\n\n"), false, err)
			return "", err
		}
		if !cached {
			allCached = false
		}
		parts = append(parts, fmt.Sprintf("[图片 %d 识别结果]\n%s", i+1, strings.TrimSpace(text)))
	}
	text := strings.Join(parts, "\n\n")
	a.recordVisionDebug(ep, pm, text, allCached, nil)
	return text, nil
}

func (a *app) describeOneImage(ctx context.Context, cfg config, ep endpoint, pm parsedMessage) (string, bool, error) {
	cacheKey := visionCacheKey(ep, cfg.VisionPrompt, pm)
	if text, ok := a.cachedVisionText(cacheKey); ok {
		return text, true, nil
	}
	var (
		text string
		err  error
	)
	switch ep.Provider {
	case "anthropic":
		text, err = a.describeWithAnthropic(ctx, ep, cfg.VisionPrompt, pm)
	case "gemini":
		text, err = a.describeWithGemini(ctx, ep, cfg.VisionPrompt, pm)
	case "ollama":
		text, err = a.describeWithOllama(ctx, ep, cfg.VisionPrompt, pm)
	default:
		text, err = a.describeWithOpenAI(ctx, ep, cfg.VisionPrompt, pm)
	}
	if err == nil && strings.TrimSpace(text) != "" {
		a.storeVisionText(cacheKey, text)
	}
	return text, false, err
}

func (a *app) recordVisionDebug(ep endpoint, pm parsedMessage, text string, cached bool, err error) {
	info := visionDebugInfo{
		At:         time.Now(),
		Provider:   ep.Provider,
		Model:      ep.ModelOverride,
		UserText:   pm.Text,
		ImageCount: len(pm.Images),
		Text:       text,
		Cached:     cached,
	}
	if err != nil {
		info.Error = err.Error()
	}
	a.mu.Lock()
	a.lastVision = info
	a.mu.Unlock()
}

func (a *app) cachedVisionText(key string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.visionCache == nil {
		return "", false
	}
	text, ok := a.visionCache[key]
	return text, ok
}

func (a *app) storeVisionText(key, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.visionCache == nil || len(a.visionCache) >= maxVisionCacheEntries {
		a.visionCache = make(map[string]string)
	}
	a.visionCache[key] = text
}

func visionCacheKey(ep endpoint, prompt string, pm parsedMessage) string {
	h := sha256.New()
	writeHashPart(h, ep.Provider)
	writeHashPart(h, ep.BaseURL)
	writeHashPart(h, ep.ModelOverride)
	writeHashPart(h, prompt)
	writeHashPart(h, pm.Text)
	for _, img := range pm.Images {
		writeHashPart(h, img.MediaType)
		writeHashPart(h, img.URL)
		writeHashPart(h, img.Base64)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func writeHashPart(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}

func visionPromptText(prompt string, userText string) string {
	return fmt.Sprintf("用户需求仅用于判断哪些视觉细节相关，不要直接回答该需求。\n用户需求：%s\n\n%s", emptyAs(userText, "描述图片。"), prompt)
}

func (a *app) describeWithOpenAI(ctx context.Context, ep endpoint, prompt string, pm parsedMessage) (string, error) {
	content := []any{map[string]any{
		"type": "text",
		"text": visionPromptText(prompt, pm.Text),
	}}
	for _, img := range pm.Images {
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": img.URL,
			},
		})
	}
	payload := map[string]any{
		"model":       ep.ModelOverride,
		"messages":    []any{map[string]any{"role": "user", "content": content}},
		"temperature": 0.1,
		"max_tokens":  1200,
	}
	body, _ := json.Marshal(payload)
	resp, err := a.forwardJSON(ctx, ep, http.MethodPost, "/v1/chat/completions", body, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision model returned %d: %s", resp.StatusCode, trimBody(raw))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("vision model returned no choices")
	}
	return contentToText(parsed.Choices[0].Message.Content), nil
}

func (a *app) describeWithAnthropic(ctx context.Context, ep endpoint, prompt string, pm parsedMessage) (string, error) {
	content := []any{map[string]any{"type": "text", "text": visionPromptText(prompt, pm.Text)}}
	for _, img := range pm.Images {
		source := map[string]any{"type": "url", "url": img.URL}
		if img.Base64 != "" {
			source = map[string]any{"type": "base64", "media_type": img.MediaType, "data": img.Base64}
		}
		content = append(content, map[string]any{"type": "image", "source": source})
	}
	payload := map[string]any{
		"model":      ep.ModelOverride,
		"max_tokens": 1200,
		"messages":   []any{map[string]any{"role": "user", "content": content}},
	}
	body, _ := json.Marshal(payload)
	resp, err := a.forwardJSON(ctx, ep, http.MethodPost, "/v1/messages", body, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision model returned %d: %s", resp.StatusCode, trimBody(raw))
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(parsed.Content))
	for _, part := range parsed.Content {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func (a *app) describeWithGemini(ctx context.Context, ep endpoint, prompt string, pm parsedMessage) (string, error) {
	parts := []any{map[string]any{"text": visionPromptText(prompt, pm.Text)}}
	for _, img := range pm.Images {
		if img.Base64 != "" {
			parts = append(parts, map[string]any{"inline_data": map[string]any{"mime_type": img.MediaType, "data": img.Base64}})
		} else {
			parts = append(parts, map[string]any{"file_data": map[string]any{"mime_type": img.MediaType, "file_uri": img.URL}})
		}
	}
	payload := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"temperature":     0.1,
			"maxOutputTokens": 1200,
		},
	}
	body, _ := json.Marshal(payload)
	path := fmt.Sprintf("/v1beta/models/%s:generateContent", url.PathEscape(ep.ModelOverride))
	resp, err := a.forwardJSON(ctx, ep, http.MethodPost, path, body, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision model returned %d: %s", resp.StatusCode, trimBody(raw))
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Candidates) == 0 {
		return "", errors.New("vision model returned no candidates")
	}
	partsText := make([]string, 0)
	for _, part := range parsed.Candidates[0].Content.Parts {
		if part.Text != "" {
			partsText = append(partsText, part.Text)
		}
	}
	return strings.Join(partsText, "\n"), nil
}

func (a *app) describeWithOllama(ctx context.Context, ep endpoint, prompt string, pm parsedMessage) (string, error) {
	images := make([]string, 0, len(pm.Images))
	for _, img := range pm.Images {
		if img.Base64 != "" {
			images = append(images, img.Base64)
		}
	}
	payload := map[string]any{
		"model":  ep.ModelOverride,
		"stream": false,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": visionPromptText(prompt, pm.Text),
			"images":  images,
		}},
	}
	body, _ := json.Marshal(payload)
	resp, err := a.forwardJSON(ctx, ep, http.MethodPost, "/api/chat", body, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision model returned %d: %s", resp.StatusCode, trimBody(raw))
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Response string `json:"response"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Message.Content != "" {
		return parsed.Message.Content, nil
	}
	return parsed.Response, nil
}
