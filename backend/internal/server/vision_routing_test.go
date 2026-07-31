package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseOpenAIContentDoesNotTreatUntypedLinksAsImages(t *testing.T) {
	pm := parseOpenAIContent([]any{
		map[string]any{"url": "https://example.com/article"},
		map[string]any{"url": "data:image/png;base64,aGVsbG8="},
	})
	if len(pm.Images) != 1 {
		t.Fatalf("parsed %d images, want only the explicit image-looking URL", len(pm.Images))
	}
	if pm.Images[0].URL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("unexpected image URL: %#v", pm.Images[0])
	}
}

func TestTextOnlyOpenAIChatDoesNotCallVision(t *testing.T) {
	visionCalls := 0
	vision := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCalls++
		http.Error(w, "vision must not be called for text-only input", http.StatusBadGateway)
	}))
	defer vision.Close()

	textCalls := 0
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		textCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode text payload: %v", err)
			return
		}
		if payload["model"] != "upstream-text" {
			t.Errorf("text model = %v, want upstream-text", payload["model"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer text.Close()

	visionOn := true
	a := &app{
		cfg: config{
			TextProvider:      "openai",
			TextBaseURL:       text.URL,
			TextAPIKey:        "text-key",
			TextModelMappings: []textModelMapping{{Name: "text-only", Model: "upstream-text", SupportsImages: false}},
			VisionProvider:    "openai",
			VisionBaseURL:     vision.URL,
			VisionAPIKey:      "vision-key",
			VisionModel:       "unavailable-vision",
			VisionEnabled:     &visionOn,
		},
		httpClient: text.Client(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"text-only","messages":[{"role":"user","content":"hello"}]}`))
	a.handleOpenAIChat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if textCalls != 1 {
		t.Fatalf("text calls = %d, want 1", textCalls)
	}
	if visionCalls != 0 {
		t.Fatalf("vision calls = %d, want 0 for text-only input", visionCalls)
	}
}
