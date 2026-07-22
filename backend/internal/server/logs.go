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
	maxLogTailSize = 64 * 1024
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status               int
	body                 bytes.Buffer
	tail                 []byte
	written              int
	started              time.Time
	firstTokenMS         int64
	downstreamGone       bool
	drainAfterDisconnect bool
}

type sseLogState struct {
	IsSSE     bool
	Completed bool
	Failed    bool
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
		w.firstTokenMS = maxInt64(1, time.Since(w.started).Milliseconds())
	}
	w.written += len(p)
	if w.body.Len() < maxLogBodySize {
		remaining := maxLogBodySize - w.body.Len()
		if len(p) > remaining {
			_, _ = w.body.Write(p[:remaining])
		} else {
			_, _ = w.body.Write(p)
		}
	}
	w.writeTail(p)
	if w.downstreamGone {
		return len(p), nil
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil || n != len(p) {
		if !w.drainAfterDisconnect {
			return n, err
		}
		// Keep draining the upstream stream so its terminal usage event can be
		// persisted even after the downstream client has disconnected.
		w.downstreamGone = true
		return len(p), nil
	}
	return n, nil
}

func (w *loggingResponseWriter) enableDisconnectDrain() {
	w.drainAfterDisconnect = true
}

func (w *loggingResponseWriter) writeTail(p []byte) {
	if len(p) == 0 {
		return
	}
	if len(p) >= maxLogTailSize {
		w.tail = append(w.tail[:0], p[len(p)-maxLogTailSize:]...)
		return
	}
	w.tail = append(w.tail, p...)
	if len(w.tail) > maxLogTailSize {
		copy(w.tail, w.tail[len(w.tail)-maxLogTailSize:])
		w.tail = w.tail[:maxLogTailSize]
	}
}

func (w *loggingResponseWriter) logBody() []byte {
	head := w.body.Bytes()
	if len(w.tail) == 0 || len(head) < maxLogBodySize {
		return head
	}
	tail := w.tail
	if overlap := len(head) + len(tail) - w.written; overlap > 0 && overlap < len(tail) {
		tail = tail[overlap:]
	} else if overlap >= len(tail) {
		return head
	}
	out := make([]byte, 0, len(head)+1+len(tail))
	out = append(out, head...)
	out = append(out, '\n')
	out = append(out, tail...)
	return out
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
	out := make([]requestLog, len(a.logs))
	for i := range a.logs {
		out[len(a.logs)-1-i] = a.logs[i]
	}
	a.logMu.Unlock()
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
	requestPayload := decodeJSONMap(body)
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return
	}
	firstTokenMS := firstInt64FromSlice(firstTokenValues)
	sseState := inspectSSELogBody(responseBody)
	if status >= 400 || !sseState.IsSSE {
		firstTokenMS = 0
	}
	log := requestLog{
		At:           started,
		Method:       r.Method,
		Path:         r.URL.RequestURI(),
		Protocol:     protocolName(r.URL.Path),
		Status:       status,
		DurationMS:   time.Since(started).Milliseconds(),
		FirstTokenMS: firstTokenMS,
		RequestMode:  requestStreamMode(r, requestPayload),
	}
	log.UpstreamName, log.UpstreamProvider = a.upstreamLogIdentity()
	if requestPayload != nil {
		log.Model = a.effectiveRequestLogModel(r, requestPayload)
	}
	if response := decodeJSONMap(responseBody); response != nil {
		fillUsageFromPayload(&log, response)
		if log.Model == "" {
			log.Model = firstString(response["model"])
		}
		log.Error = errorTextFromPayload(response)
	} else {
		fillUsageFromSSE(&log, responseBody)
	}
	if status < 400 && isOpenAIResponsesPath(r.URL.Path) && sseState.IsSSE {
		switch {
		case sseState.Failed:
			if log.Error == "" {
				log.Error = "上游 Responses 响应流失败"
			}
		case !sseState.Completed || !hasTokenUsageLog(log):
			return
		}
	}
	if status >= 400 && log.Error == "" {
		log.Error = statusText(status)
	}
	log.Status = status
	a.appendRequestLog(log)
}

func hasTokenUsageLog(log requestLog) bool {
	return log.InputTokens > 0 || log.OutputTokens > 0 || log.TotalTokens > 0 || log.CacheHitTokens > 0 || log.CacheWriteTokens > 0
}

func isSSELogBody(body []byte) bool {
	return inspectSSELogBody(body).IsSSE
}

func inspectSSELogBody(body []byte) sseLogState {
	var state sseLogState
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event:") {
			state.IsSSE = true
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		state.IsSSE = true
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			state.Completed = true
			continue
		}
		var payload map[string]any
		if data == "" || json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		switch strings.ToLower(firstString(payload["type"])) {
		case "response.completed", "response.done":
			state.Completed = true
		case "response.incomplete":
			state.Completed = true
		case "error", "response.failed":
			state.Failed = true
		}
	}
	return state
}

func (a *app) upstreamLogIdentity() (string, string) {
	cfg := a.currentConfig()
	for _, profile := range cfg.TextModelProfiles {
		if profile.ID == cfg.ActiveTextProfileID {
			return firstString(profile.Name, "未命名文本模型"), normalizeProvider(profile.Provider)
		}
	}
	return firstString(cfg.TextModelOverride, "当前文本上游"), normalizeProvider(cfg.TextProvider)
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

func requestStreamMode(r *http.Request, payload map[string]any) string {
	path := ""
	if r != nil && r.URL != nil {
		path = r.URL.Path
	}
	if strings.Contains(path, ":streamGenerateContent") {
		return "stream"
	}
	if stream, exists := payload["stream"].(bool); exists {
		if stream {
			return "stream"
		}
		return "sync"
	}
	if isOllamaChatPath(path) || isOllamaGeneratePath(path) {
		return "stream"
	}
	if r != nil && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		return "stream"
	}
	return "sync"
}

func (a *app) effectiveRequestLogModel(r *http.Request, payload map[string]any) string {
	requested := requestModel(payload)
	if isGeminiGeneratePath(r.URL.Path) {
		requested = firstString(geminiRequestedModel(r.URL.RequestURI()), requested)
	}
	switch {
	case isOpenAIChatPath(r.URL.Path),
		isOpenAIResponsesPath(r.URL.Path),
		isAnthropicMessagesPath(r.URL.Path),
		isGeminiGeneratePath(r.URL.Path),
		isOllamaChatPath(r.URL.Path),
		isOllamaGeneratePath(r.URL.Path):
		if model := effectiveTextModel(a.currentConfig(), requested); model != "" {
			return model
		}
	}
	return requested
}

func requestTextFromPayload(protocol string, payload map[string]any) string {
	switch protocol {
	case "Chat Completions":
		return trimText(contentToText(payload["messages"]))
	case "Responses":
		return trimText(contentToText(payload["input"]))
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
	if response, _ := payload["response"].(map[string]any); response != nil {
		if text := errorTextFromPayload(response); text != "" {
			return text
		}
	}
	errValue, ok := payload["error"]
	if ok && errValue != nil {
		switch v := errValue.(type) {
		case string:
			text := strings.TrimSpace(v)
			if strings.EqualFold(text, "null") || strings.EqualFold(text, "<nil>") {
				return ""
			}
			return text
		case map[string]any:
			return firstString(v["message"], v["type"], v["code"])
		default:
			b, _ := json.Marshal(v)
			text := strings.TrimSpace(string(b))
			if strings.EqualFold(text, "null") {
				return ""
			}
			return text
		}
	}
	switch strings.ToLower(firstString(payload["type"])) {
	case "error", "response.failed":
		return firstString(payload["message"], payload["code"], payload["type"])
	}
	return ""
}

func statusText(status int) string {
	if status == 0 {
		return ""
	}
	return "HTTP " + strconv.Itoa(status)
}

func fillUsageFromPayload(log *requestLog, payload map[string]any) {
	var separateCacheTokens int64
	if log.Model == "" {
		log.Model = firstString(payload["model"])
	}
	if response, _ := payload["response"].(map[string]any); response != nil {
		fillUsageFromPayload(log, response)
	}
	if usage, _ := payload["usage"].(map[string]any); usage != nil {
		setIfNonZero(&log.InputTokens, firstInt64(usage["prompt_tokens"], usage["input_tokens"]))
		setIfNonZero(&log.OutputTokens, firstInt64(usage["completion_tokens"], usage["output_tokens"]))
		setIfNonZero(&log.TotalTokens, firstInt64(usage["total_tokens"]))
		if details, _ := usage["prompt_tokens_details"].(map[string]any); details != nil {
			log.CacheHitTokens += firstInt64(details["cached_tokens"], details["cache_read_tokens"])
			log.CacheWriteTokens += firstInt64(details["cache_creation_tokens"], details["cache_write_tokens"])
		}
		if details, _ := usage["input_tokens_details"].(map[string]any); details != nil {
			log.CacheHitTokens += firstInt64(details["cached_tokens"], details["cache_read_tokens"])
			log.CacheWriteTokens += firstInt64(details["cache_creation_tokens"], details["cache_write_tokens"])
		}
		separateCacheReadTokens := firstInt64(usage["cache_read_input_tokens"], usage["cache_read_tokens"])
		separateCacheWriteTokens := firstInt64(usage["cache_creation_input_tokens"], usage["cache_creation_tokens"], usage["cache_write_input_tokens"], usage["cache_write_tokens"])
		log.CacheHitTokens += separateCacheReadTokens
		log.CacheWriteTokens += separateCacheWriteTokens
		separateCacheTokens += separateCacheReadTokens + separateCacheWriteTokens
	}
	if usage, _ := payload["usageMetadata"].(map[string]any); usage != nil {
		setIfNonZero(&log.InputTokens, firstInt64(usage["promptTokenCount"]))
		setIfNonZero(&log.OutputTokens, firstInt64(usage["candidatesTokenCount"]))
		setIfNonZero(&log.TotalTokens, firstInt64(usage["totalTokenCount"]))
		setIfNonZero(&log.CacheHitTokens, firstInt64(usage["cachedContentTokenCount"]))
	}
	if usage, _ := payload["usage"].(map[string]any); usage != nil {
		setIfNonZero(&log.InputTokens, firstInt64(usage["input_tokens"]))
		setIfNonZero(&log.OutputTokens, firstInt64(usage["output_tokens"]))
	}
	setIfNonZero(&log.InputTokens, firstInt64(payload["prompt_eval_count"]))
	setIfNonZero(&log.OutputTokens, firstInt64(payload["eval_count"]))
	if log.TotalTokens == 0 {
		// Anthropic-style top-level cache fields are separate from input_tokens.
		// Cached-token details nested under OpenAI-compatible input/prompt usage
		// are already included in input_tokens and must not be counted twice.
		log.TotalTokens = log.InputTokens + log.OutputTokens + separateCacheTokens
	}
}

func setIfNonZero(target *int64, value int64) {
	if value != 0 {
		*target = value
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func trimText(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= 1200 {
		return text
	}
	return string([]rune(text)[:1200]) + "..."
}
