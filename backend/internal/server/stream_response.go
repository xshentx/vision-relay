package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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

const maxOpenAIResponsesSSEEventBytes = 8 << 20

var errOpenAIResponsesSSEEventTooLarge = errors.New("upstream Responses SSE event exceeded the relay size limit")

type openAIResponsesStreamState struct {
	terminal   bool
	failed     bool
	responseID string
	model      string
	createdAt  int64
	eventName  string
	dataLines  []string
}

// writeOpenAIResponsesStream proxies native Responses SSE while enforcing the
// protocol's terminal-event contract. A plain io.Copy turns an upstream EOF or
// read timeout into a silent downstream close, which Codex can only describe as
// "stream closed before response.completed". Emitting response.failed preserves
// the real relay/upstream cause and makes the failure visible in request logs.
func writeOpenAIResponsesStream(w http.ResponseWriter, resp *http.Response) {
	writeOpenAIResponsesStreamWithLimit(w, resp, maxOpenAIResponsesSSEEventBytes)
}

func writeOpenAIResponsesStreamWithLimit(w http.ResponseWriter, resp *http.Response, maxEventBytes int) {
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	beginProtocolValidation(resp)
	reader := bufio.NewReader(resp.Body)
	flusher, _ := w.(http.Flusher)
	state := openAIResponsesStreamState{}
	var pendingEvent []byte
	lastLineEndedWithNewline := true

	for {
		remaining := maxEventBytes - len(pendingEvent)
		line, lineEndedWithNewline, readErr := readLimitedSSELine(reader, remaining)
		if len(line) != 0 {
			pendingEvent = append(pendingEvent, line...)
			lastLineEndedWithNewline = lineEndedWithNewline
			state.observeLine(line)
		}

		if readErr == nil && isSSEBlankLine(line) {
			if !writeBufferedSSEEvent(w, pendingEvent) {
				return
			}
			pendingEvent = nil
			if flusher != nil {
				flusher.Flush()
			}
			if state.terminal {
				reportOpenAIResponsesTerminalState(resp, state)
				return
			}
			continue
		}

		if readErr == nil {
			continue
		}

		if errors.Is(readErr, errOpenAIResponsesSSEEventTooLarge) {
			failure := errOpenAIResponsesSSEEventTooLarge
			reportProviderStreamFailure(resp, failure)
			// Complete events have already been forwarded, but pendingEvent has not.
			// Dropping the oversized pending event avoids exposing a partial SSE frame.
			writeOpenAIResponsesFailureEvent(w, flusher, state, failure.Error(), true, true)
			return
		}

		if len(pendingEvent) != 0 {
			// EOF also dispatches a final SSE event for relay validation. Complete its
			// downstream delimiter explicitly because compatible gateways sometimes
			// omit the terminating blank line.
			state.finishEvent()
			if !writeBufferedSSEEvent(w, pendingEvent) {
				return
			}
		}

		if state.terminal {
			reportOpenAIResponsesTerminalState(resp, state)
			completeSSEEventBoundary(w, len(pendingEvent) == 0, lastLineEndedWithNewline)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		failure := responsesStreamReadFailure(resp, readErr)
		reportProviderStreamFailure(resp, failure)
		writeOpenAIResponsesFailureEvent(w, flusher, state, failure.Error(), len(pendingEvent) == 0, lastLineEndedWithNewline)
		return
	}
}

// readLimitedSSELine returns one complete LF-delimited line without allowing a
// line (and therefore its containing event) to grow memory without bound.
func readLimitedSSELine(reader *bufio.Reader, limit int) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, false, errOpenAIResponsesSSEEventTooLarge
	}
	line := make([]byte, 0, min(limit, reader.Size()))
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if len(line)+len(fragment) > limit {
			return nil, false, errOpenAIResponsesSSEEventTooLarge
		}
		line = append(line, fragment...)
		switch {
		case readErr == nil:
			return line, true, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			return line, false, io.EOF
		default:
			return line, false, readErr
		}
	}
}

func writeBufferedSSEEvent(w io.Writer, event []byte) bool {
	if len(event) == 0 {
		return true
	}
	_, err := w.Write(event)
	return err == nil
}

func isSSEBlankLine(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return len(line) == 0
}

func (s *openAIResponsesStreamState) observeLine(rawLine []byte) {
	line := bytes.TrimSuffix(rawLine, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		s.finishEvent()
		return
	}
	if line[0] == ':' {
		return
	}

	field, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		value = nil
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	switch string(field) {
	case "event":
		s.eventName = string(value)
	case "data":
		s.dataLines = append(s.dataLines, string(value))
	}
}

func (s *openAIResponsesStreamState) finishEvent() {
	eventName := s.eventName
	data := strings.Join(s.dataLines, "\n")
	s.eventName = ""
	s.dataLines = nil

	// Native Responses streams terminate with response.completed,
	// response.incomplete, or response.failed. A chat-style [DONE] marker alone
	// does not satisfy the contract expected by Responses clients.
	if data == "" || data == "[DONE]" {
		return
	}
	var event map[string]any
	if json.Unmarshal([]byte(data), &event) != nil {
		return
	}
	if response, _ := event["response"].(map[string]any); response != nil {
		s.responseID = firstString(response["id"], s.responseID)
		s.model = firstString(response["model"], s.model)
		if created := firstInt64(response["created_at"], response["created"]); created != 0 {
			s.createdAt = created
		}
	}
	eventType := firstString(event["type"])
	if eventType == "" {
		eventType = eventName
	}
	switch strings.ToLower(eventType) {
	case "response.completed", "response.done", "response.incomplete":
		s.terminal = true
	case "response.failed", "error":
		s.terminal = true
		s.failed = true
	}
}

func reportOpenAIResponsesTerminalState(resp *http.Response, state openAIResponsesStreamState) {
	if state.failed {
		reportProviderStreamFailure(resp, errors.New("upstream Responses stream reported a failure"))
		return
	}
	reportProviderStreamSuccess(resp)
}

func responsesStreamReadFailure(resp *http.Response, readErr error) error {
	if errors.Is(readErr, io.EOF) {
		return errors.New("upstream Responses stream ended before a terminal event")
	}
	if resp != nil && resp.Request != nil && resp.Request.Context().Err() != nil {
		return fmt.Errorf("local relay canceled the upstream Responses stream before completion: %w", readErr)
	}
	return fmt.Errorf("upstream Responses stream read failed before completion: %w", readErr)
}

func completeSSEEventBoundary(w io.Writer, atEventBoundary, lastLineEndedWithNewline bool) {
	if atEventBoundary {
		return
	}
	if lastLineEndedWithNewline {
		_, _ = io.WriteString(w, "\n")
		return
	}
	_, _ = io.WriteString(w, "\n\n")
}

func writeOpenAIResponsesFailureEvent(w http.ResponseWriter, flusher http.Flusher, state openAIResponsesStreamState, message string, atEventBoundary, lastLineEndedWithNewline bool) {
	completeSSEEventBoundary(w, atEventBoundary, lastLineEndedWithNewline)
	responseID := state.responseID
	if responseID == "" {
		responseID = "resp_vision_relay_" + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	createdAt := state.createdAt
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	event := map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": createdAt,
			"status":     "failed",
			"model":      state.model,
			"error": map[string]any{
				"type":    "server_error",
				"code":    "upstream_stream_error",
				"message": message,
			},
		},
	}
	encoded, _ := json.Marshal(event)
	_, _ = io.WriteString(w, "event: response.failed\ndata: ")
	_, _ = w.Write(encoded)
	_, _ = io.WriteString(w, "\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func reportProviderStreamFailure(resp *http.Response, err error) {
	if resp != nil && resp.Body != nil {
		if observer, ok := resp.Body.(providerProtocolObserver); ok {
			observer.reportProviderFailure(err)
		}
	}
}

func reportProviderStreamSuccess(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		if observer, ok := resp.Body.(providerProtocolObserver); ok {
			observer.reportProviderSuccess()
		}
	}
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
	reportProviderSuccess()
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

func (r *readCloser) reportProviderSuccess() {
	if observer, ok := r.Closer.(providerProtocolObserver); ok {
		observer.reportProviderSuccess()
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
