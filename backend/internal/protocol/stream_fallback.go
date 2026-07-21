package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// WriteGeminiStreamFromSyncResponse turns one completed generateContent result
// into a one-chunk streamGenerateContent response.
func WriteGeminiStreamFromSyncResponse(w http.ResponseWriter, resp *http.Response, sse bool) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	body = bytes.TrimSpace(body)
	chunks := []json.RawMessage{json.RawMessage(body)}
	if len(body) > 0 && body[0] == '[' {
		if err := json.Unmarshal(body, &chunks); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(bytes.TrimSpace(chunk))
			_, _ = w.Write([]byte("\n\n"))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(body) > 0 && body[0] == '[' {
		_, _ = w.Write(body)
		_, _ = w.Write([]byte("\n"))
		return
	}
	_, _ = w.Write([]byte("["))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("]\n"))
}

// WriteOllamaStreamFromSyncResponse turns a completed Ollama response into a
// valid one-record NDJSON stream.
func WriteOllamaStreamFromSyncResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.TrimSpace(body))
	_, _ = w.Write([]byte("\n"))
}
