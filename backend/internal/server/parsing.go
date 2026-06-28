package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

func decodeMessages(v any) ([]message, error) {
	raw, ok := v.([]any)
	if !ok {
		return nil, errors.New("messages must be an array")
	}
	out := make([]message, 0, len(raw))
	for _, item := range raw {
		b, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var msg message
		if err := json.Unmarshal(b, &msg); err != nil {
			return nil, err
		}
		if msg.Role == "" {
			return nil, errors.New("message role is required")
		}
		out = append(out, msg)
	}
	return out, nil
}

func parseOpenAIMessage(msg message) parsedMessage {
	pm := parseOpenAIContent(msg.Content)
	pm.Message = msg
	return pm
}

func parseOpenAIContent(content any) parsedMessage {
	pm := parsedMessage{}
	switch c := content.(type) {
	case string:
		pm.Text = c
	case map[string]any:
		parseOpenAIContentPart(c, &pm, nil)
	case []any:
		textParts := make([]string, 0)
		for _, part := range c {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			parseOpenAIContentPart(partMap, &pm, &textParts)
		}
		pm.Text = strings.Join(textParts, "\n")
	}
	return pm
}

func parseOpenAIContentPart(part map[string]any, pm *parsedMessage, textParts *[]string) {
	partType, _ := part["type"].(string)
	switch partType {
	case "text", "input_text", "output_text":
		appendTextPart(firstString(part["text"], part["content"]), pm, textParts)
	case "image_url", "input_image":
		appendImageFromAny(part["image_url"], pm)
		appendImageFromAny(part["imageUrl"], pm)
		appendImageFromAny(part["url"], pm)
		appendImageFilePart(part, pm)
	case "image", "input_file", "file":
		appendImageFilePart(part, pm)
	case "":
		appendTextPart(firstString(part["text"], part["content"]), pm, textParts)
		appendImageFromAny(part["image_url"], pm)
		appendImageFromAny(part["imageUrl"], pm)
		appendImageFromAny(part["url"], pm)
		appendImageFilePart(part, pm)
	}
}

func appendTextPart(text string, pm *parsedMessage, textParts *[]string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if textParts != nil {
		*textParts = append(*textParts, text)
		return
	}
	pm.Text = strings.TrimSpace(pm.Text + "\n" + text)
}

func appendImageFromAny(value any, pm *parsedMessage) {
	switch imageURL := value.(type) {
	case string:
		if imageURL != "" {
			pm.Images = append(pm.Images, newImageRef(imageURL, "", ""))
		}
	case map[string]any:
		mediaType := mediaTypeFromMap(imageURL)
		if rawURL := firstString(imageURL["url"], imageURL["uri"], imageURL["file_uri"], imageURL["fileUri"], imageURL["file_data"], imageURL["fileData"], ""); rawURL != "" {
			pm.Images = append(pm.Images, newImageRef(rawURL, mediaType, ""))
		}
		if data := firstString(imageURL["data"], imageURL["base64"], imageURL["content"], ""); data != "" {
			pm.Images = append(pm.Images, newImageRef("", mediaType, data))
		}
	}
}

func appendImageFilePart(part map[string]any, pm *parsedMessage) {
	mediaType := mediaTypeFromMap(part)
	filename := firstString(part["filename"], part["file_name"], part["name"])
	rawURL := firstString(part["url"], part["uri"], part["file_uri"], part["fileUri"], part["file_data"], part["fileData"], "")
	data := firstString(part["data"], part["base64"], part["content"], "")
	if mediaType != "" && !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return
	}
	if mediaType == "" && !looksLikeImageName(filename) && !looksLikeImageData(rawURL) && !looksLikeImageData(data) {
		if nested, ok := part["file"].(map[string]any); ok {
			appendImageFilePart(nested, pm)
		}
		if nested, ok := part["source"].(map[string]any); ok {
			appendImageFilePart(nested, pm)
		}
		return
	}
	if rawURL != "" {
		pm.Images = append(pm.Images, newImageRef(rawURL, mediaType, ""))
	}
	if data != "" {
		pm.Images = append(pm.Images, newImageRef("", mediaType, data))
	}
	if nested, ok := part["file"].(map[string]any); ok {
		appendImageFilePart(nested, pm)
	}
	if nested, ok := part["source"].(map[string]any); ok {
		appendImageFilePart(nested, pm)
	}
}

func mediaTypeFromMap(value map[string]any) string {
	return firstString(value["media_type"], value["mediaType"], value["mime_type"], value["mimeType"], value["mime"], value["content_type"], value["contentType"], "")
}

func looksLikeImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

func looksLikeImageData(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "data:image/")
}

func parseAnthropicContent(content any) parsedMessage {
	pm := parsedMessage{}
	switch c := content.(type) {
	case string:
		pm.Text = c
	case []any:
		textParts := make([]string, 0)
		for _, part := range c {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			switch partType {
			case "text":
				if text, ok := partMap["text"].(string); ok && strings.TrimSpace(text) != "" {
					textParts = append(textParts, text)
				}
			case "image":
				if source, ok := partMap["source"].(map[string]any); ok {
					mediaType, _ := source["media_type"].(string)
					switch source["type"] {
					case "base64":
						if data, ok := source["data"].(string); ok && data != "" {
							pm.Images = append(pm.Images, newImageRef("", mediaType, data))
						}
					case "url":
						if rawURL, ok := source["url"].(string); ok && rawURL != "" {
							pm.Images = append(pm.Images, newImageRef(rawURL, mediaType, ""))
						}
					}
				}
			}
		}
		pm.Text = strings.Join(textParts, "\n")
	}
	return pm
}

func parseGeminiParts(partsValue any) parsedMessage {
	parts, ok := partsValue.([]any)
	if !ok {
		return parsedMessage{}
	}
	pm := parsedMessage{}
	textParts := make([]string, 0)
	for _, part := range parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := partMap["text"].(string); ok && strings.TrimSpace(text) != "" {
			textParts = append(textParts, text)
		}
		for _, key := range []string{"inline_data", "inlineData"} {
			if inline, ok := partMap[key].(map[string]any); ok {
				mediaType := firstString(inline["mime_type"], inline["mimeType"], "image/png")
				if data, ok := inline["data"].(string); ok && data != "" {
					pm.Images = append(pm.Images, newImageRef("", mediaType, data))
				}
			}
		}
		for _, key := range []string{"file_data", "fileData"} {
			if fileData, ok := partMap[key].(map[string]any); ok {
				mediaType := firstString(fileData["mime_type"], fileData["mimeType"], "image/png")
				if fileURI, ok := fileData["file_uri"].(string); ok && fileURI != "" {
					pm.Images = append(pm.Images, newImageRef(fileURI, mediaType, ""))
				}
				if fileURI, ok := fileData["fileUri"].(string); ok && fileURI != "" {
					pm.Images = append(pm.Images, newImageRef(fileURI, mediaType, ""))
				}
			}
		}
	}
	pm.Text = strings.Join(textParts, "\n")
	return pm
}

func parseOllamaMessage(msg map[string]any) parsedMessage {
	pm := parsedMessage{}
	if text, ok := msg["content"].(string); ok {
		pm.Text = text
	}
	if images, ok := msg["images"].([]any); ok {
		for _, image := range images {
			if data, ok := image.(string); ok && data != "" {
				pm.Images = append(pm.Images, newImageRef("", "image/png", data))
			}
		}
	}
	return pm
}

func parseOllamaGenerate(payload map[string]any) parsedMessage {
	pm := parsedMessage{}
	if text, ok := payload["prompt"].(string); ok {
		pm.Text = text
	}
	if images, ok := payload["images"].([]any); ok {
		for _, image := range images {
			if data, ok := image.(string); ok && data != "" {
				pm.Images = append(pm.Images, newImageRef("", "image/png", data))
			}
		}
	}
	return pm
}

func newImageRef(rawURL, mediaType, data string) imageRef {
	img := imageRef{URL: rawURL, MediaType: emptyAs(mediaType, "image/png"), Base64: data}
	if rawURL != "" {
		if mt, b64, ok := parseDataURL(rawURL); ok {
			img.MediaType = mt
			img.Base64 = b64
		}
	}
	if img.URL == "" && img.Base64 != "" {
		img.URL = "data:" + img.MediaType + ";base64," + img.Base64
	}
	return img
}

func parseDataURL(raw string) (string, string, bool) {
	if !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return "", "", false
	}
	meta := raw[5:comma]
	data := raw[comma+1:]
	if !strings.Contains(meta, ";base64") {
		decoded, err := url.QueryUnescape(data)
		if err != nil {
			return "", "", false
		}
		data = base64.StdEncoding.EncodeToString([]byte(decoded))
	}
	mediaType := strings.Split(meta, ";")[0]
	if mediaType == "" {
		mediaType = "image/png"
	}
	return mediaType, data, true
}

func encodeMessages(parsed []parsedMessage) []any {
	out := make([]any, 0, len(parsed))
	for _, pm := range parsed {
		out = append(out, map[string]any{
			"role":    pm.Message.Role,
			"content": pm.Message.Content,
		})
	}
	return out
}

func buildAugmentedContent(userText, analysis string) string {
	userText = strings.TrimSpace(userText)
	analysis = strings.TrimSpace(analysis)
	if userText == "" {
		return "[图片识别结果，仅供文本模型参考，最终回答由文本模型完成]\n" + analysis
	}
	return userText + "\n\n[图片识别结果，仅供文本模型参考，最终回答由文本模型完成]\n" + analysis
}
