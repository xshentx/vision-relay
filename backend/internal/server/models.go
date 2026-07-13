package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type modelListRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	ProxyURL string `json:"proxy_url"`
}

type modelListItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *app) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.handleRawProxy(w, r)
		return
	}
	cfg := a.currentConfig()
	if len(textModelOverrides(cfg)) > 0 {
		writeJSON(w, http.StatusOK, augmentModelListPayload(defaultModelListPayload(cfg), cfg))
		return
	}
	resp, err := a.forwardRaw(r.Context(), a.textEndpoint(cfg), http.MethodGet, canonicalRequestURI(r.URL.RequestURI()), nil, r.Header)
	if err != nil {
		writeJSON(w, http.StatusOK, augmentModelListPayload(defaultModelListPayload(cfg), cfg))
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusOK, augmentModelListPayload(defaultModelListPayload(cfg), cfg))
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeJSON(w, http.StatusOK, augmentModelListPayload(defaultModelListPayload(cfg), cfg))
		return
	}
	writeJSON(w, http.StatusOK, augmentModelListPayload(payload, cfg))
}

func (a *app) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req modelListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	models, err := a.fetchProviderModels(r, req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"count":  len(models),
	})
}

func defaultModelListPayload(cfg config) map[string]any {
	mappings := textModelMappings(cfg)
	if len(mappings) == 0 {
		mappings = []textModelMapping{{Name: "local-text-model", Model: "local-text-model"}}
	}
	data := make([]any, 0, len(mappings))
	for _, mapping := range mappings {
		model := map[string]any{
			"id":        mapping.Name,
			"object":    "model",
			"created":   time.Now().Unix(),
			"owned_by":  appSlug,
			"reasoning": textModelSupportsReasoning(mapping),
		}
		if mapping.ContextWindow > 0 {
			model["context_window"] = mapping.ContextWindow
			model["max_context_window"] = mapping.ContextWindow
		}
		data = append(data, model)
	}
	return map[string]any{
		"object": "list",
		"data":   data,
	}
}

func augmentModelListPayload(payload map[string]any, cfg config) map[string]any {
	data, _ := payload["data"].([]any)
	for _, item := range data {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if relayImageInputEnabled(cfg) {
			markModelImageCapable(model)
		}
	}
	if _, ok := payload["object"]; !ok {
		payload["object"] = "list"
	}
	return payload
}

func markModelImageCapable(model map[string]any) {
	model["attachment"] = true
	model["attachments"] = true
	model["vision"] = true
	model["supports_attachments"] = true
	model["supports_images"] = true
	model["modalities"] = map[string]any{
		"input":  []any{"text", "image"},
		"output": []any{"text"},
	}
	model["input_modalities"] = []any{"text", "image"}
	model["output_modalities"] = []any{"text"}
	model["supported_input_modalities"] = []any{"text", "image"}
	model["supported_output_modalities"] = []any{"text"}
	capabilities, _ := model["capabilities"].(map[string]any)
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	capabilities["vision"] = true
	capabilities["image"] = true
	capabilities["images"] = true
	capabilities["attachments"] = true
	model["capabilities"] = capabilities
}

func (a *app) fetchProviderModels(r *http.Request, req modelListRequest) ([]modelListItem, error) {
	provider := normalizeProvider(req.Provider)
	ep := endpoint{
		Provider: provider,
		BaseURL:  strings.TrimSpace(req.BaseURL),
		APIKey:   strings.TrimSpace(req.APIKey),
		ProxyURL: strings.TrimSpace(req.ProxyURL),
	}
	path := modelListPath(provider)
	target, err := joinTargetURL(firstString(ep.BaseURL, defaultBaseURL(provider)), path)
	if err != nil {
		return nil, err
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Accept", "application/json")
	applyProviderAuth(upstreamReq, ep, nil)
	client, err := a.upstreamHTTPClient(ep.ProxyURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("models upstream returned " + resp.Status)
	}
	var payload any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return extractModelItems(provider, payload), nil
}

func modelListPath(provider string) string {
	switch normalizeProvider(provider) {
	case "gemini":
		return "/v1beta/models"
	case "ollama":
		return "/api/tags"
	default:
		return "/v1/models"
	}
}

func extractModelItems(provider string, payload any) []modelListItem {
	root, _ := payload.(map[string]any)
	var raw []any
	switch normalizeProvider(provider) {
	case "gemini", "ollama":
		raw, _ = root["models"].([]any)
	default:
		raw, _ = root["data"].([]any)
		if len(raw) == 0 {
			raw, _ = root["models"].([]any)
		}
	}
	seen := map[string]bool{}
	items := make([]modelListItem, 0, len(raw))
	for _, item := range raw {
		id, name := modelIDAndName(provider, item)
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" || seen[id] {
			continue
		}
		if name == "" {
			name = id
		}
		seen[id] = true
		items = append(items, modelListItem{ID: id, Name: name})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].ID) < strings.ToLower(items[j].ID)
	})
	return items
}

func modelIDAndName(provider string, item any) (string, string) {
	switch v := item.(type) {
	case string:
		return normalizeModelListID(provider, v), v
	case map[string]any:
		id := firstString(v["id"], v["name"], v["model"])
		name := firstString(v["display_name"], v["displayName"], v["name"], v["id"], v["model"])
		return normalizeModelListID(provider, id), name
	default:
		return "", ""
	}
}

func normalizeModelListID(provider, id string) string {
	id = strings.TrimSpace(id)
	if normalizeProvider(provider) == "gemini" {
		id = strings.TrimPrefix(id, "models/")
	}
	return id
}
