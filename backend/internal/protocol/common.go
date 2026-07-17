package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func contentToText(content any) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		parts := make([]string, 0)
		for _, item := range value {
			if itemMap, ok := item.(map[string]any); ok {
				if text, ok := itemMap["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if nested, ok := itemMap["content"]; ok {
					if text := contentToText(nested); text != "" {
						parts = append(parts, text)
						continue
					}
				}
				if nested, ok := itemMap["parts"]; ok {
					if text := contentToText(nested); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text, ok := value["text"].(string); ok {
			return text
		}
		for _, key := range []string{"content", "parts", "message"} {
			if nested, ok := value[key]; ok {
				return contentToText(nested)
			}
		}
		if response, ok := value["response"].(string); ok {
			return response
		}
	}
	encoded, _ := json.Marshal(content)
	return string(encoded)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func trimBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 1200 {
		return text[:1200] + "..."
	}
	return text
}

func writeError(w http.ResponseWriter, status int, err error) {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    "vision_relay_error",
		},
	})
}

func imageFileURL(part map[string]any) string {
	mediaType := firstString(part["media_type"], part["mediaType"], part["mime_type"], part["mimeType"], part["content_type"], part["contentType"])
	filename := firstString(part["filename"], part["file_name"], part["name"])
	rawURL := firstString(part["url"], part["uri"], part["file_uri"], part["fileUri"], part["file_data"], part["fileData"])
	data := firstString(part["data"], part["base64"], part["content"])
	if mediaType != "" && !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return ""
	}
	if mediaType == "" && !looksLikeImageName(filename) && !strings.HasPrefix(strings.TrimSpace(rawURL), "data:image/") && !strings.HasPrefix(strings.TrimSpace(data), "data:image/") {
		for _, key := range []string{"file", "source"} {
			if nested, ok := part[key].(map[string]any); ok {
				if value := imageFileURL(nested); value != "" {
					return value
				}
			}
		}
		return ""
	}
	if rawURL != "" {
		return rawURL
	}
	if data != "" {
		if strings.HasPrefix(data, "data:image/") {
			return data
		}
		if mediaType == "" {
			mediaType = "image/png"
		}
		return fmt.Sprintf("data:%s;base64,%s", mediaType, data)
	}
	for _, key := range []string{"file", "source"} {
		if nested, ok := part[key].(map[string]any); ok {
			if value := imageFileURL(nested); value != "" {
				return value
			}
		}
	}
	return ""
}

func looksLikeImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

const maxStreamEventSize = 256 * 1024

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Encoding") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func firstInt64(values ...any) int64 {
	for _, value := range values {
		if number := numberAsInt64(value); number != 0 {
			return number
		}
	}
	return 0
}
