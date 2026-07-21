package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const responsesFallbackHeader = "X-Vision-Relay-Stream-Fallback"

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		return true
	}
	return responseStartsWithSSE(resp)
}

// responseStartsWithSSE recognizes a stream even when a compatible gateway
// omits or mislabels Content-Type. It restores every byte consumed while
// sniffing so callers can still proxy the response without buffering it.
func responseStartsWithSSE(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	reader := bufio.NewReader(resp.Body)
	prefix := make([]byte, 0, 16)
	field := make([]byte, 0, 8)
	for len(prefix) < 4096 {
		value, err := reader.ReadByte()
		if err != nil {
			restoreSniffedResponseBody(resp, reader, prefix)
			return false
		}
		prefix = append(prefix, value)
		if len(field) == 0 && (value == ' ' || value == '\t' || value == '\r' || value == '\n') {
			continue
		}
		if len(field) == 0 && value == ':' {
			restoreSniffedResponseBody(resp, reader, prefix)
			return true
		}
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		field = append(field, value)
		text := string(field)
		possible := false
		for _, candidate := range []string{"data:", "event:", "id:", "retry:"} {
			if strings.HasPrefix(candidate, text) {
				possible = true
			}
			if strings.HasPrefix(text, candidate) {
				restoreSniffedResponseBody(resp, reader, prefix)
				return true
			}
		}
		if !possible {
			restoreSniffedResponseBody(resp, reader, prefix)
			return false
		}
	}
	restoreSniffedResponseBody(resp, reader, prefix)
	return false
}

func restoreSniffedResponseBody(resp *http.Response, reader *bufio.Reader, prefix []byte) {
	original := resp.Body
	resp.Body = &readCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: original}
}

func isSuccessfulResponse(resp *http.Response) bool {
	return resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
}

// shouldRetryResponseSynchronously is deliberately narrow. Authentication,
// rate-limit, and transient server failures are returned as-is; only statuses
// commonly used for an unsupported streaming request are retried.
func shouldRetryResponseSynchronously(resp *http.Response) bool {
	if resp == nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusNotAcceptable,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity,
		http.StatusNotImplemented,
		http.StatusHTTPVersionNotSupported:
		return true
	default:
		return false
	}
}

func synchronousPayload(payload map[string]any) []byte {
	payload["stream"] = false
	delete(payload, "stream_options")
	body, _ := json.Marshal(payload)
	return body
}

func synchronousRequestHeaders(original http.Header) http.Header {
	header := original.Clone()
	header.Set("Accept", "application/json")
	return header
}

func isNDJSONResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	return strings.Contains(contentType, "ndjson") || strings.Contains(contentType, "json-seq")
}

func ollamaStreamRequested(payload map[string]any) bool {
	value, exists := payload["stream"]
	if !exists {
		return true
	}
	stream, ok := value.(bool)
	return ok && stream
}

func geminiStreamingRequest(requestURI string) bool {
	parsed, err := url.Parse(requestURI)
	if err != nil {
		return strings.Contains(requestURI, ":streamGenerateContent")
	}
	return strings.HasSuffix(parsed.Path, ":streamGenerateContent")
}

func geminiSSERequested(requestURI string, header http.Header) bool {
	parsed, err := url.Parse(requestURI)
	if err == nil && strings.EqualFold(parsed.Query().Get("alt"), "sse") {
		return true
	}
	return strings.Contains(strings.ToLower(header.Get("Accept")), "text/event-stream")
}

func geminiSynchronousRequestURI(requestURI string) string {
	parsed, err := url.Parse(requestURI)
	if err != nil {
		return strings.Replace(requestURI, ":streamGenerateContent", ":generateContent", 1)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, ":streamGenerateContent") + ":generateContent"
	query := parsed.Query()
	query.Del("alt")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// responseStartsWithJSONArray checks only the first non-space byte and restores
// the response body, so a genuine Gemini JSON-array stream is not buffered.
func responseStartsWithJSONArray(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	reader := bufio.NewReader(resp.Body)
	prefix := make([]byte, 0, 16)
	for len(prefix) < 4096 {
		value, err := reader.ReadByte()
		if err != nil {
			resp.Body = &readCloser{Reader: bytes.NewReader(prefix), Closer: resp.Body}
			return false
		}
		prefix = append(prefix, value)
		if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
			continue
		}
		resp.Body = &readCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: resp.Body}
		return value == '['
	}
	resp.Body = &readCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: resp.Body}
	return false
}

type readCloser struct {
	io.Reader
	io.Closer
}
