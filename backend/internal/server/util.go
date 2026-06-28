package server

import (
	"strings"
	"unicode/utf8"
)

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func estimateTokens(text string) int64 {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return int64(runes/3 + 1)
}

func trimBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 1200 {
		return text[:1200] + "..."
	}
	return text
}
