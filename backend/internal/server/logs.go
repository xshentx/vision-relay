package server

import (
	"bytes"
	"encoding/json"
	"io"
	stdlog "log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxLogs        = 300
	maxLogBodySize = 256 * 1024
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status       int
	body         bytes.Buffer
	started      time.Time
	firstTokenMS int64
}

func newLoggingResponseWriter(w http.ResponseWriter, started time.Time) *loggingResponseWriter {
	return &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK, started: started}
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(p []byte) (int, error) {
	if len(p) > 0 && w.firstTokenMS == 0 {
		w.firstTokenMS = time.Since(w.started).Milliseconds()
	}
	if w.body.Len() < maxLogBodySize {
		remaining := maxLogBodySize - w.body.Len()
		if len(p) > remaining {
			_, _ = w.body.Write(p[:remaining])
		} else {
			_, _ = w.body.Write(p)
		}
	}
	return w.ResponseWriter.Write(p)
}

func (a *app) handleLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, pageSize := logPageParams(r)
		logs, total := a.currentLogsPage(page, pageSize)
		writeJSON(w, http.StatusOK, map[string]any{
			"logs":      logs,
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		})
	case http.MethodDelete:
		a.clearLogs()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func logPageParams(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	switch {
	case pageSize <= 0:
		pageSize = 20
	case pageSize > 100:
		pageSize = 100
	}
	return page, pageSize
}

func (a *app) currentLogsPage(page, pageSize int) ([]requestLog, int) {
	offset := (page - 1) * pageSize
	if a.db != nil {
		total, countErr := countRequestLogsDB(a.db)
		logs, listErr := listRequestLogsPageDB(a.db, pageSize, offset)
		if countErr == nil && listErr == nil {
			return logs, total
		}
		if countErr != nil {
			stdlog.Printf("database logs count warning: %v", countErr)
		}
		if listErr != nil {
			stdlog.Printf("database logs read warning: %v", listErr)
		}
	}
	all := a.currentLogs()
	total := len(all)
	if offset >= total {
		return []requestLog{}, total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return all[offset:end], total
}

func (a *app) currentLogs() []requestLog {
	if a.db != nil {
		logs, err := listRequestLogsDB(a.db, maxLogs)
		if err == nil {
			return logs
		}
		stdlog.Printf("database logs read warning: %v", err)
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	out := make([]requestLog, len(a.logs))
	for i := range a.logs {
		out[len(a.logs)-1-i] = a.logs[i]
	}
	return out
}

func (a *app) clearLogs() {
	if a.db != nil {
		if err := clearRequestLogsDB(a.db); err != nil {
			stdlog.Printf("database logs clear warning: %v", err)
		}
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logs = nil
}

func (a *app) appendRequestLog(log requestLog) {
	if a.db != nil {
		if err := insertRequestLogDB(a.db, log); err != nil {
			stdlog.Printf("database log write warning: %v", err)
		}
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.nextLogID++
	log.ID = a.nextLogID
	a.logs = append(a.logs, log)
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
}

func (a *app) logCompletedRequest(r *http.Request, body, responseBody []byte, status int, started time.Time, firstTokenValues ...int64) {
	log := requestLog{
		At:           started,
		Method:       r.Method,
		Path:         r.URL.RequestURI(),
		Protocol:     protocolName(r.URL.Path),
		Status:       status,
		DurationMS:   time.Since(started).Milliseconds(),
		FirstTokenMS: firstInt64FromSlice(firstTokenValues),
	}
	log.ClientName, log.ClientKeyPreview = a.clientLogIdentity(r)
	if payload := decodeJSONMap(body); payload != nil {
		log.Model = requestModel(payload)
	}
	if response := decodeJSONMap(responseBody); response != nil {
		fillUsageFromPayload(&log, response)
		if log.Model == "" {
			log.Model = firstString(response["model"])
		}
		log.Error = errorTextFromPayload(response)
	} else {
		fillUsageFromSSE(&log, responseBody)
		if log.Error == "" && status >= 400 {
			log.Error = statusText(status)
		}
	}
	if status >= 400 && log.Error == "" {
		log.Error = statusText(status)
	}
	a.appendRequestLog(log)
}

func captureRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (a *app) clientLogIdentity(r *http.Request) (string, string) {
	candidates := audienceKeys(r.Header)
	if key := strings.TrimSpace(r.URL.Query().Get("key")); key != "" {
		candidates = append(candidates, key)
	}
	cfg := a.currentConfig()
	for _, got := range candidates {
		for _, entry := range cfg.ClientAPIKeyEntries {
			if got == entry.Key {
				return firstString(entry.Name, "未命名客户端"), keyPreview(got)
			}
		}
	}
	if len(candidates) > 0 {
		return "未匹配密钥", keyPreview(candidates[0])
	}
	if len(cfg.ClientAPIKeyEntries) == 0 {
		return "未启用鉴权", ""
	}
	return "未提供密钥", ""
}

func keyPreview(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 10 {
		return key
	}
	return key[:3] + "..." + key[len(key)-6:]
}

func protocolName(path string) string {
	switch {
	case isOpenAIChatPath(path):
		return "Chat Completions"
	case isOpenAIResponsesPath(path):
		return "Responses"
	case isAnthropicMessagesPath(path):
		return "Anthropic Messages"
	case isGeminiGeneratePath(path):
		return "Gemini"
	case isOllamaChatPath(path):
		return "Ollama Chat"
	case isOllamaGeneratePath(path):
		return "Ollama Generate"
	default:
		return "Raw Proxy"
	}
}

func decodeJSONMap(body []byte) map[string]any {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	return payload
}

func requestModel(payload map[string]any) string {
	return firstString(payload["model"])
}

func requestTextFromPayload(protocol string, payload map[string]any) string {
	switch protocol {
	case "Chat Completions":
		return trimText(contentToText(payload["messages"]))
	case "Responses":
		return trimText(responsesContentToText(payload["input"]))
	case "Anthropic Messages":
		return trimText(contentToText(payload["messages"]))
	case "Gemini":
		return trimText(contentToText(payload["contents"]))
	case "Ollama Generate":
		return trimText(firstString(payload["prompt"]))
	case "Ollama Chat":
		return trimText(contentToText(payload["messages"]))
	default:
		b, _ := json.Marshal(payload)
		return trimText(string(b))
	}
}

func responseTextFromPayload(payload map[string]any) string {
	if text := firstString(payload["output_text"], payload["response"]); text != "" {
		return trimText(text)
	}
	if choices, _ := payload["choices"].([]any); len(choices) > 0 {
		if choice, _ := choices[0].(map[string]any); choice != nil {
			if message, _ := choice["message"].(map[string]any); message != nil {
				return trimText(contentToText(message["content"]))
			}
		}
	}
	if content, ok := payload["content"]; ok {
		return trimText(contentToText(content))
	}
	if candidates, _ := payload["candidates"].([]any); len(candidates) > 0 {
		return trimText(contentToText(candidates))
	}
	if message, _ := payload["message"].(map[string]any); message != nil {
		return trimText(contentToText(message["content"]))
	}
	return ""
}

func errorTextFromPayload(payload map[string]any) string {
	errValue, ok := payload["error"]
	if !ok {
		return ""
	}
	switch v := errValue.(type) {
	case string:
		return v
	case map[string]any:
		return firstString(v["message"], v["type"], v["code"])
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func statusText(status int) string {
	if status == 0 {
		return ""
	}
	return "HTTP " + strconv.Itoa(status)
}

func fillUsageFromPayload(log *requestLog, payload map[string]any) {
	if usage, _ := payload["usage"].(map[string]any); usage != nil {
		log.InputTokens = firstInt64(usage["prompt_tokens"], usage["input_tokens"])
		log.OutputTokens = firstInt64(usage["completion_tokens"], usage["output_tokens"])
		log.TotalTokens = firstInt64(usage["total_tokens"])
		if log.TotalTokens == 0 {
			log.TotalTokens = log.InputTokens + log.OutputTokens
		}
		if details, _ := usage["prompt_tokens_details"].(map[string]any); details != nil {
			log.CacheHitTokens += numberAsInt64(details["cached_tokens"])
		}
		if details, _ := usage["input_tokens_details"].(map[string]any); details != nil {
			log.CacheHitTokens += numberAsInt64(details["cached_tokens"])
		}
		log.CacheHitTokens += numberAsInt64(usage["cache_read_input_tokens"])
	}
	if usage, _ := payload["usageMetadata"].(map[string]any); usage != nil {
		log.InputTokens = firstInt64(usage["promptTokenCount"])
		log.OutputTokens = firstInt64(usage["candidatesTokenCount"])
		log.TotalTokens = firstInt64(usage["totalTokenCount"])
		log.CacheHitTokens = firstInt64(usage["cachedContentTokenCount"])
	}
	if usage, _ := payload["usage"].(map[string]any); usage != nil {
		log.InputTokens = firstInt64(log.InputTokens, usage["input_tokens"])
		log.OutputTokens = firstInt64(log.OutputTokens, usage["output_tokens"])
	}
	log.InputTokens = firstInt64(log.InputTokens, payload["prompt_eval_count"])
	log.OutputTokens = firstInt64(log.OutputTokens, payload["eval_count"])
	if log.TotalTokens == 0 {
		log.TotalTokens = log.InputTokens + log.OutputTokens
	}
}

func fillUsageFromSSE(log *requestLog, body []byte) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		if log.Model == "" {
			log.Model = firstString(payload["model"])
		}
		before := log.TotalTokens
		fillUsageFromPayload(log, payload)
		if log.TotalTokens != 0 && log.TotalTokens != before {
			continue
		}
		if errText := errorTextFromPayload(payload); errText != "" {
			log.Error = errText
		}
	}
}

func ensureStreamUsage(payload map[string]any) {
	stream, _ := payload["stream"].(bool)
	if !stream {
		return
	}
	options, _ := payload["stream_options"].(map[string]any)
	if options == nil {
		options = map[string]any{}
		payload["stream_options"] = options
	}
	if _, exists := options["include_usage"]; !exists {
		options["include_usage"] = true
	}
}

func firstInt64(values ...any) int64 {
	for _, value := range values {
		if n := numberAsInt64(value); n != 0 {
			return n
		}
	}
	return 0
}

func firstInt64FromSlice(values []int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func trimText(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= 1200 {
		return text
	}
	return string([]rune(text)[:1200]) + "..."
}
