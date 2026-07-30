package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	beginProtocolValidation(resp)
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		acceptProtocolValidation(resp)
		return true
	}
	if responseStartsWithSSE(resp) {
		acceptProtocolValidation(resp)
		return true
	}
	return false
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

func writeUnexpectedStreamingResponse(w http.ResponseWriter, resp *http.Response, expected string) {
	contentType := ""
	if resp != nil {
		contentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	}
	if contentType == "" {
		contentType = "missing"
	}
	protocolErr := fmt.Errorf("upstream returned a non-streaming response for a streaming request: expected %s, got Content-Type %q", expected, contentType)
	if resp != nil && resp.Body != nil {
		if reporter, ok := resp.Body.(providerProtocolObserver); ok {
			reporter.reportProviderFailure(protocolErr)
		}
		_ = resp.Body.Close()
	}
	writeError(w, http.StatusBadGateway, protocolErr)
}

func isNDJSONResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	beginProtocolValidation(resp)
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	valid := strings.Contains(contentType, "ndjson") || strings.Contains(contentType, "json-seq")
	if valid {
		acceptProtocolValidation(resp)
	}
	return valid
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

// responseStartsWithJSONArray checks only the first non-space byte and restores
// the response body, so a genuine Gemini JSON-array stream is not buffered.
func responseStartsWithJSONArray(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	beginProtocolValidation(resp)
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
		valid := value == '['
		if valid {
			acceptProtocolValidation(resp)
		}
		return valid
	}
	resp.Body = &readCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: resp.Body}
	return false
}

type providerProtocolObserver interface {
	beginProtocolValidation()
	acceptProtocolValidation()
	reportProviderFailure(error)
}

func beginProtocolValidation(resp *http.Response) {
	if !isSuccessfulResponse(resp) || resp.Body == nil {
		return
	}
	if observer, ok := resp.Body.(providerProtocolObserver); ok {
		observer.beginProtocolValidation()
	}
}

func acceptProtocolValidation(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if observer, ok := resp.Body.(providerProtocolObserver); ok {
		observer.acceptProtocolValidation()
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}

func (r *readCloser) reportProviderFailure(err error) {
	if observer, ok := r.Closer.(providerProtocolObserver); ok {
		observer.reportProviderFailure(err)
	}
}

func (r *readCloser) beginProtocolValidation() {
	if observer, ok := r.Closer.(providerProtocolObserver); ok {
		observer.beginProtocolValidation()
	}
}

func (r *readCloser) acceptProtocolValidation() {
	if observer, ok := r.Closer.(providerProtocolObserver); ok {
		observer.acceptProtocolValidation()
	}
}
