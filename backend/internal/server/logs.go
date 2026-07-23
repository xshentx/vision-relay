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
	log.UpstreamName, log.UpstreamProvider = a.upstreamLogIdentityForRequest(r)
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
	return upstreamLogIdentityFromConfig(a.currentConfig())
}

func (a *app) upstreamLogIdentityForRequest(r *http.Request) (string, string) {
	if r != nil {
		if selection, ok := providerRouteTraceFromContext(r.Context()).get(); ok {
			return firstString(selection.Name, "\u5f53\u524d\u6587\u672c\u4e0a\u6e38"), normalizeProvider(selection.Provider)
		}
	}
	return upstreamLogIdentityFromConfig(a.textConfigForRequest(r))
}

func upstreamLogIdentityFromConfig(cfg config) (string, string) {
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
		if model := effectiveTextModel(a.textConfigForRequest(r), requested); model != "" {
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

// tokenUsageSnapshot is deliberately a snapshot rather than an accumulator.
// A streaming provider may repeat the same usage object in several SSE events;
// adding each object is therefore incorrect. Protocol-specific collectors use
// the authoritative final snapshot, while envelope fields are merged without
// ever summing duplicated counters.
type tokenUsageSnapshot struct {
	Input            int64
	Output           int64
	Total            int64
	CacheRead        int64
	CacheWrite       int64
	HasInput         bool
	HasOutput        bool
	HasTotal         bool
	HasCacheRead     bool
	HasCacheWrite    bool
	HasSeparateCache bool
}

func fillUsageFromPayload(log *requestLog, payload map[string]any) {
	if log.Model == "" {
		log.Model = firstString(payload["model"])
	}
	applyTokenUsageSnapshot(log, collectTokenUsage(payload))
}

func collectTokenUsage(payload map[string]any) tokenUsageSnapshot {
	var usage tokenUsageSnapshot
	if response, _ := payload["response"].(map[string]any); response != nil {
		usage = mergeTokenUsageSnapshots(usage, collectTokenUsage(response))
	}
	if raw, _ := payload["usage"].(map[string]any); raw != nil {
		input, hasInput := firstPresentInt64(raw["prompt_tokens"], raw["input_tokens"])
		output, hasOutput := firstPresentInt64(raw["completion_tokens"], raw["output_tokens"])
		total, hasTotal := firstPresentInt64(raw["total_tokens"])
		current := tokenUsageSnapshot{
			Input: input, Output: output, Total: total,
			HasInput: hasInput, HasOutput: hasOutput, HasTotal: hasTotal,
		}
		// Prefer top-level cache fields, then one nested details object. Some
		// relays expose both aliases with the same value; they describe one
		// cache read and must never be added together. Presence wins over value,
		// so an explicit zero is not replaced by a lower-priority alias.
		inputDetails, _ := raw["input_tokens_details"].(map[string]any)
		promptDetails, _ := raw["prompt_tokens_details"].(map[string]any)
		current.CacheRead, current.HasCacheRead = firstPresentInt64(
			raw["cache_read_input_tokens"], raw["cache_read_tokens"],
			inputDetails["cached_tokens"], inputDetails["cache_read_tokens"],
			promptDetails["cached_tokens"], promptDetails["cache_read_tokens"],
		)
		current.CacheWrite, current.HasCacheWrite = firstPresentInt64(
			raw["cache_creation_input_tokens"], raw["cache_creation_tokens"],
			raw["cache_write_input_tokens"], raw["cache_write_tokens"],
			inputDetails["cache_creation_tokens"], inputDetails["cache_write_tokens"],
			promptDetails["cache_creation_tokens"], promptDetails["cache_write_tokens"],
		)
		current.HasSeparateCache = hasAnyTokenField(raw,
			"cache_read_input_tokens", "cache_read_tokens",
			"cache_creation_input_tokens", "cache_creation_tokens",
			"cache_write_input_tokens", "cache_write_tokens",
		)
		usage = mergeTokenUsageSnapshots(usage, current)
	}
	if raw, _ := payload["usageMetadata"].(map[string]any); raw != nil {
		input, hasInput := firstPresentInt64(raw["promptTokenCount"])
		total, hasTotal := firstPresentInt64(raw["totalTokenCount"])
		output, hasOutput := firstPresentInt64(raw["candidatesTokenCount"])
		// Gemini's totalTokenCount includes candidates and thoughts. Match
		// cc-switch by deriving output from total - input when total is present.
		if hasTotal && hasInput {
			output = maxInt64(total-input, 0)
			hasOutput = true
		}
		cacheRead, hasCacheRead := firstPresentInt64(raw["cachedContentTokenCount"])
		usage = mergeTokenUsageSnapshots(usage, tokenUsageSnapshot{
			Input: input, Output: output, Total: total, CacheRead: cacheRead,
			HasInput: hasInput, HasOutput: hasOutput, HasTotal: hasTotal,
			HasCacheRead: hasCacheRead,
		})
	}
	if input, ok := firstPresentInt64(payload["prompt_eval_count"]); ok {
		if !usage.HasInput || input > usage.Input {
			usage.Input, usage.HasInput = input, true
		}
	}
	if output, ok := firstPresentInt64(payload["eval_count"]); ok {
		if !usage.HasOutput || output > usage.Output {
			usage.Output, usage.HasOutput = output, true
		}
	}
	return usage
}

func hasAnyTokenField(payload map[string]any, fields ...string) bool {
	for _, field := range fields {
		if _, ok := payload[field]; ok {
			return true
		}
	}
	return false
}

func mergeTokenUsageSnapshots(left, right tokenUsageSnapshot) tokenUsageSnapshot {
	return tokenUsageSnapshot{
		Input:            maxInt64(left.Input, right.Input),
		Output:           maxInt64(left.Output, right.Output),
		Total:            maxInt64(left.Total, right.Total),
		CacheRead:        maxInt64(left.CacheRead, right.CacheRead),
		CacheWrite:       maxInt64(left.CacheWrite, right.CacheWrite),
		HasInput:         left.HasInput || right.HasInput,
		HasOutput:        left.HasOutput || right.HasOutput,
		HasTotal:         left.HasTotal || right.HasTotal,
		HasCacheRead:     left.HasCacheRead || right.HasCacheRead,
		HasCacheWrite:    left.HasCacheWrite || right.HasCacheWrite,
		HasSeparateCache: left.HasSeparateCache || right.HasSeparateCache,
	}
}

func applyTokenUsageSnapshot(log *requestLog, usage tokenUsageSnapshot) {
	log.InputTokens = maxInt64(log.InputTokens, usage.Input)
	log.OutputTokens = maxInt64(log.OutputTokens, usage.Output)
	log.TotalTokens = maxInt64(log.TotalTokens, usage.Total)
	log.CacheHitTokens = maxInt64(log.CacheHitTokens, usage.CacheRead)
	log.CacheWriteTokens = maxInt64(log.CacheWriteTokens, usage.CacheWrite)
	if log.TotalTokens == 0 {
		// OpenAI-compatible input_tokens already includes cache reads, even when
		// a provider also exposes Anthropic-style cache_* aliases. Anthropic's
		// cache counters are separate billable input and are included in total.
		provider := strings.TrimSpace(log.UpstreamProvider)
		// Anthropic Messages responses use fresh input plus separate cache
		// counters, including responses synthesized from an OpenAI upstream.
		separate := usage.HasSeparateCache &&
			(log.Protocol == "Anthropic Messages" || provider == "" || normalizeProvider(provider) != "openai")
		log.TotalTokens = log.InputTokens + log.OutputTokens
		if separate {
			log.TotalTokens += log.CacheHitTokens + log.CacheWriteTokens
		}
	}
}

func fillUsageFromSSE(log *requestLog, body []byte) {
	events := make([]map[string]any, 0)
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
		events = append(events, payload)
		if log.Model == "" {
			log.Model = firstString(payload["model"])
			if response, _ := payload["response"].(map[string]any); response != nil {
				log.Model = firstString(response["model"], log.Model)
			}
		}
		if errText := errorTextFromPayload(payload); errText != "" {
			log.Error = errText
		}
	}

	// Responses API has an authoritative terminal usage object. Ignore earlier
	// deltas so an implementation that repeats usage cannot inflate the result.
	for i := len(events) - 1; i >= 0; i-- {
		typeName := strings.ToLower(firstString(events[i]["type"]))
		if typeName == "response.completed" || typeName == "response.done" || typeName == "response.incomplete" {
			fillUsageFromPayload(log, events[i])
			return
		}
	}

	// Anthropic splits usage between message_start and message_delta. Match
	// cc-switch's pair-aware merge: a later smaller input is the fresh-input
	// correction, and its cache counters must be adopted as a pair instead of
	// being max-merged with the initial context usage.
	var anthropic tokenUsageSnapshot
	seenAnthropic := false
	inputFromDelta := false
	for _, event := range events {
		typeName := strings.ToLower(firstString(event["type"]))
		switch typeName {
		case "message_start":
			seenAnthropic = true
			message, _ := event["message"].(map[string]any)
			if message != nil {
				if log.Model == "" {
					log.Model = firstString(message["model"])
				}
				anthropic = collectTokenUsage(message)
			}
		case "message_delta":
			seenAnthropic = true
			current := collectTokenUsage(event)
			if current.HasOutput {
				anthropic.Output, anthropic.HasOutput = current.Output, true
			}
			if current.HasTotal {
				anthropic.Total, anthropic.HasTotal = current.Total, true
			}
			if current.HasInput {
				input := current.Input
				shouldUseDelta := !anthropic.HasInput || input < anthropic.Input ||
					(inputFromDelta && input <= anthropic.Input)
				if shouldUseDelta {
					anthropic.Input, anthropic.HasInput = input, true
					inputFromDelta = true
					if current.HasCacheRead {
						anthropic.CacheRead, anthropic.HasCacheRead = current.CacheRead, true
					}
					if current.HasCacheWrite {
						anthropic.CacheWrite, anthropic.HasCacheWrite = current.CacheWrite, true
					}
				}
			}
			// Some compatible providers only attach cache fields to the delta.
			// Keep them as a best-effort fallback when the start event had none.
			if anthropic.CacheRead == 0 && current.HasCacheRead {
				anthropic.CacheRead, anthropic.HasCacheRead = current.CacheRead, true
			}
			if anthropic.CacheWrite == 0 && current.HasCacheWrite {
				anthropic.CacheWrite, anthropic.HasCacheWrite = current.CacheWrite, true
			}
			anthropic.HasSeparateCache = anthropic.HasSeparateCache || current.HasSeparateCache
		}
	}
	if seenAnthropic {
		applyTokenUsageSnapshot(log, anthropic)
		return
	}

	// OpenAI Chat, Gemini and Ollama providers emit a cumulative usage object
	// in their final usage-bearing event. Use that final snapshot, matching
	// cc-switch, instead of taking a per-field maximum across unrelated
	// intermediate snapshots.
	for i := len(events) - 1; i >= 0; i-- {
		if hasTokenUsagePayload(events[i]) {
			applyTokenUsageSnapshot(log, collectTokenUsage(events[i]))
			return
		}
	}
	applyTokenUsageSnapshot(log, tokenUsageSnapshot{})
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

func hasTokenUsagePayload(payload map[string]any) bool {
	if _, ok := payload["usage"].(map[string]any); ok {
		return true
	}
	if _, ok := payload["usageMetadata"].(map[string]any); ok {
		return true
	}
	for _, key := range []string{"prompt_eval_count", "eval_count"} {
		if _, ok := firstPresentInt64(payload[key]); ok {
			return true
		}
	}
	return false
}

func firstPresentInt64(values ...any) (int64, bool) {
	for _, value := range values {
		if value == nil {
			continue
		}
		switch value.(type) {
		case int, int64, float64, json.Number:
			return numberAsInt64(value), true
		}
	}
	return 0, false
}

func firstInt64(values ...any) int64 {
	value, _ := firstPresentInt64(values...)
	return value
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
