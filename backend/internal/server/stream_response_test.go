package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUnexpectedStreamingResponseRecordsProviderFailureThroughSniffWrapper(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		validate func(*http.Response) bool
	}{
		{name: "synchronous JSON", body: `{"id":"completed-sync"}`, validate: isEventStreamResponse},
		{name: "empty body", body: "", validate: isEventStreamResponse},
		{name: "whitespace only", body: " \t\r\n", validate: isEventStreamResponse},
		{name: "incomplete SSE prefix", body: "data", validate: isEventStreamResponse},
		{
			name: "empty Gemini body after nested sniffing",
			body: "",
			validate: func(resp *http.Response) bool {
				return isEventStreamResponse(resp) || responseStartsWithJSONArray(resp)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newProviderRouter()
			candidate := providerRouteCandidate{Group: providerGroupCodex, ProfileID: "non-streaming-provider"}

			for attempt := 0; attempt < providerFailureThreshold; attempt++ {
				selected := mustSelectProviderTestCandidate(t, router, candidate)
				body := newProviderObservedBody(
					context.Background(),
					io.NopCloser(strings.NewReader(test.body)),
					router,
					selected,
				)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       body,
				}
				if test.validate(resp) {
					t.Fatal("non-streaming response was detected as a valid stream")
				}

				rec := httptest.NewRecorder()
				writeUnexpectedStreamingResponse(rec, resp, "expected streaming protocol")
				if rec.Code != http.StatusBadGateway {
					t.Fatalf("attempt %d status = %d, want %d", attempt+1, rec.Code, http.StatusBadGateway)
				}
			}

			router.mu.Lock()
			state := *router.providerStateLocked(candidate.Group, candidate.ProfileID)
			router.mu.Unlock()
			if state.FailureCount != providerFailureThreshold || state.ConsecutiveFailures != providerFailureThreshold {
				t.Fatalf("protocol failures were not recorded: %#v", state)
			}
			if state.CircuitState != providerCircuitOpen {
				t.Fatalf("circuit state = %q, want %q", state.CircuitState, providerCircuitOpen)
			}
			if !state.LastSuccessAt.IsZero() {
				t.Fatalf("invalid streaming response was recorded as a success at %v", state.LastSuccessAt)
			}
			if !strings.Contains(state.LastError, "non-streaming response for a streaming request") {
				t.Fatalf("last error does not describe the protocol mismatch: %q", state.LastError)
			}
		})
	}
}

func TestZeroLengthStreamingResponseIsObservedBeforeProviderSuccess(t *testing.T) {
	transport := providerRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Type": {"application/json"}},
			Body:          http.NoBody,
			ContentLength: 0,
		}, nil
	})
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "empty-stream", Client: "codex", Provider: "openai", WireAPI: "responses", BaseURL: "https://upstream.invalid",
	}}, map[string]string{"codex": "empty-stream"})
	a := &app{cfg: cfg, httpClient: &http.Client{Transport: transport}}

	for attempt := 0; attempt < providerFailureThreshold; attempt++ {
		parent := withProviderRouteContext(context.Background(), providerGroupCodex)
		ctx, _, release := upstreamStreamContext(parent, true)
		resp, err := a.forwardRaw(ctx, a.textEndpoint(textConfigForClient(cfg, "codex")), http.MethodPost,
			"/v1/responses", []byte(`{"model":"requested","stream":true}`), nil)
		if err != nil {
			release()
			t.Fatal(err)
		}
		if isEventStreamResponse(resp) {
			release()
			t.Fatal("empty response was detected as SSE")
		}
		rec := httptest.NewRecorder()
		writeUnexpectedStreamingResponse(rec, resp, "OpenAI Responses SSE")
		release()
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, rec.Code, http.StatusBadGateway)
		}
	}

	status := findProviderStatus(t, a.providerRouterStatus(), "codex", "empty-stream")
	if status.FailureCount != providerFailureThreshold || status.ConsecutiveFailure != providerFailureThreshold {
		t.Fatalf("zero-length protocol failures were not recorded: %#v", status)
	}
	if status.CircuitState != providerCircuitOpen || status.LastSuccessAt != nil {
		t.Fatalf("zero-length streaming responses were treated as successful: %#v", status)
	}
}

func TestStreamingRequestDoesNotUseHTTPClientTotalTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_slow\"}}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_slow\",\"status\":\"completed\"}}\n\n")
	}))
	defer upstream.Close()

	a := &app{httpClient: upstream.Client()}
	a.httpClient.Timeout = 20 * time.Millisecond
	ctx, _, release := upstreamStreamContext(context.Background(), true)
	defer release()

	resp, err := a.forwardRawOnce(ctx, endpoint{Provider: "openai", BaseURL: upstream.URL}, http.MethodPost, "/v1/responses", []byte(`{"stream":true}`), nil)
	if err != nil {
		t.Fatalf("stream inherited the HTTP client's total timeout: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading stream failed: %v", err)
	}
	if !strings.Contains(string(body), "response.completed") {
		t.Fatalf("unexpected stream body: %q", body)
	}
}

func TestNonStreamingRequestKeepsHTTPClientTotalTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, `{\"status\":\"completed\"}`)
	}))
	defer upstream.Close()

	a := &app{httpClient: upstream.Client()}
	a.httpClient.Timeout = 20 * time.Millisecond
	_, err := a.forwardRawOnce(context.Background(), endpoint{Provider: "openai", BaseURL: upstream.URL}, http.MethodPost, "/v1/responses", []byte(`{"stream":false}`), nil)
	if err == nil {
		t.Fatal("non-streaming request unexpectedly lost its total timeout")
	}
}

func TestOpenAIResponsesStreamConvertsPrematureEOFToFailedEvent(t *testing.T) {
	const upstreamBody = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cut_off\",\"model\":\"test\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	body := rec.Body.String()
	if !strings.HasPrefix(body, upstreamBody) {
		t.Fatalf("upstream events were changed: %q", body)
	}
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, `"code":"upstream_stream_error"`) {
		t.Fatalf("premature EOF was not converted to response.failed: %q", body)
	}
	if !strings.Contains(body, "upstream Responses stream ended before a terminal event") {
		t.Fatalf("failure does not identify an upstream premature close: %q", body)
	}
	state := inspectSSELogBody([]byte(body))
	if !state.IsSSE || !state.Failed || state.Completed {
		t.Fatalf("failure stream was not diagnosable by request logging: %#v", state)
	}
}

func TestOpenAIResponsesStreamTerminalEventPassesThroughUnchanged(t *testing.T) {
	const upstreamBody = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_ok\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"status\":\"completed\"}}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	if got := rec.Body.String(); got != upstreamBody {
		t.Fatalf("completed Responses stream changed:\n got %q\nwant %q", got, upstreamBody)
	}
}

func TestOpenAIResponsesStreamTerminalEventWithoutFinalNewlineCompletesBoundary(t *testing.T) {
	const upstreamBody = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\"}}"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	if got, want := rec.Body.String(), upstreamBody+"\n\n"; got != want {
		t.Fatalf("unterminated completed event boundary is wrong:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(rec.Body.String(), "response.failed") {
		t.Fatalf("completed event was incorrectly converted to failure: %q", rec.Body.String())
	}
}

func TestOpenAIResponsesStreamPrematureEOFWithoutFinalNewlineSeparatesFailureEvent(t *testing.T) {
	const upstreamBody = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	separator := `"delta":"partial"}` + "\n\nevent: response.failed\n"
	if !strings.Contains(rec.Body.String(), separator) {
		t.Fatalf("synthetic failure event is not separated from the unterminated upstream event: %q", rec.Body.String())
	}
}

func TestOpenAIResponsesStreamRecognizesMultilineDataTerminalEvent(t *testing.T) {
	const upstreamBody = "event: response.completed\n" +
		"data: {\"type\":\n" +
		"data: \"response.completed\",\"response\":{\"id\":\"resp_ok\"}}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	if got := rec.Body.String(); got != upstreamBody {
		t.Fatalf("multiline terminal event changed or was not recognized:\n got %q\nwant %q", got, upstreamBody)
	}
}

func TestOpenAIResponsesStreamUsesEventNameWhenJSONTypeIsMissing(t *testing.T) {
	const upstreamBody = "event: response.completed\ndata: {\"response\":{\"id\":\"resp_ok\"}}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStream(rec, resp)

	if got := rec.Body.String(); got != upstreamBody {
		t.Fatalf("event-name terminal fallback failed:\n got %q\nwant %q", got, upstreamBody)
	}
}

func TestOpenAIResponsesStreamRejectsOversizedEventWithoutForwardingPartialFrame(t *testing.T) {
	router := newProviderRouter()
	candidate := providerRouteCandidate{Group: providerGroupCodex, ProfileID: "oversized-stream"}
	candidate = mustSelectProviderTestCandidate(t, router, candidate)
	const oversizedData = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	observed := newProviderObservedBody(
		withProviderRouteContext(context.Background(), providerGroupCodex),
		io.NopCloser(strings.NewReader("data: "+oversizedData+"\n\n")),
		router,
		candidate,
	)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       observed,
	}
	rec := httptest.NewRecorder()

	writeOpenAIResponsesStreamWithLimit(rec, resp, 32)

	body := rec.Body.String()
	if strings.Contains(body, oversizedData) {
		t.Fatalf("part of an oversized event was forwarded: %q", body)
	}
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, errOpenAIResponsesSSEEventTooLarge.Error()) {
		t.Fatalf("oversized event did not produce a diagnostic response.failed event: %q", body)
	}

	router.mu.Lock()
	state := *router.providerStateLocked(candidate.Group, candidate.ProfileID)
	router.mu.Unlock()
	if state.FailureCount != 1 || state.ConsecutiveFailures != 1 {
		t.Fatalf("oversized event was not recorded exactly once as provider failure: %#v", state)
	}
	if state.LastError != errOpenAIResponsesSSEEventTooLarge.Error() {
		t.Fatalf("oversized event provider failure is not diagnostic: %q", state.LastError)
	}
	if !state.LastSuccessAt.IsZero() {
		t.Fatalf("oversized event was also recorded as provider success: %#v", state)
	}
}

func TestOpenAIResponsesPrematureEOFRecordsProviderFailure(t *testing.T) {
	router := newProviderRouter()
	candidate := providerRouteCandidate{Group: providerGroupCodex, ProfileID: "truncated-stream"}
	candidate = mustSelectProviderTestCandidate(t, router, candidate)
	observed := newProviderObservedBody(
		withProviderRouteContext(context.Background(), providerGroupCodex),
		io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cut_off\"}}\n\n")),
		router,
		candidate,
	)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       observed,
	}

	writeOpenAIResponsesStream(httptest.NewRecorder(), resp)

	router.mu.Lock()
	state := *router.providerStateLocked(candidate.Group, candidate.ProfileID)
	router.mu.Unlock()
	if state.FailureCount != 1 || state.ConsecutiveFailures != 1 {
		t.Fatalf("premature stream close was not recorded as provider failure: %#v", state)
	}
	if !strings.Contains(state.LastError, "ended before a terminal event") {
		t.Fatalf("provider failure is not diagnostic: %q", state.LastError)
	}
	if !state.LastSuccessAt.IsZero() {
		t.Fatalf("premature stream close was also recorded as success: %#v", state)
	}
}

func TestLoggingResponseWriterPreservesSSEFlush(t *testing.T) {
	underlying := httptest.NewRecorder()
	writer := newLoggingResponseWriter(underlying, time.Now())
	flusher, ok := any(writer).(http.Flusher)
	if !ok {
		t.Fatal("logging response writer does not expose http.Flusher")
	}
	flusher.Flush()
	if !underlying.Flushed {
		t.Fatal("SSE flush was not forwarded to the underlying response writer")
	}
}
