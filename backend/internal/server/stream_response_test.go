package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
				body := newProviderObservedBody(
					context.Background(),
					io.NopCloser(strings.NewReader(test.body)),
					router,
					candidate,
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
