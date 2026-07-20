package server

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"
)

type dashboardQuery struct {
	Period   string
	Supplier string
	Model    string
}

type dashboardSummary struct {
	LifetimeTokens int64   `json:"lifetime_tokens"`
	TodayTokens    int64   `json:"today_tokens"`
	PeriodTokens   int64   `json:"period_tokens"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CacheHitTokens int64   `json:"cache_hit_tokens"`
	Requests       int64   `json:"requests"`
	Failures       int64   `json:"failures"`
	AverageFirstMS float64 `json:"average_first_token_ms"`
	AverageMS      float64 `json:"average_duration_ms"`
}

type dashboardBucket struct {
	Key            string           `json:"key"`
	Label          string           `json:"label"`
	InputTokens    int64            `json:"input_tokens"`
	OutputTokens   int64            `json:"output_tokens"`
	CacheHitTokens int64            `json:"cache_hit_tokens"`
	TotalTokens    int64            `json:"total_tokens"`
	Requests       int64            `json:"requests"`
	Models         map[string]int64 `json:"models"`
}

type dashboardModelUsage struct {
	SeriesKey      string `json:"series_key"`
	Model          string `json:"model"`
	Supplier       string `json:"supplier"`
	InputTokens    int64  `json:"input_tokens"`
	OutputTokens   int64  `json:"output_tokens"`
	CacheHitTokens int64  `json:"cache_hit_tokens"`
	TotalTokens    int64  `json:"total_tokens"`
	Requests       int64  `json:"requests"`
}

type dashboardMonthlyAggregate struct {
	Month           string
	Supplier        string
	Model           string
	InputTokens     int64
	OutputTokens    int64
	CacheHitTokens  int64
	TotalTokens     int64
	Requests        int64
	Failures        int64
	DurationTotal   int64
	FirstTokenTotal int64
	FirstTokenCount int64
	TodayTokens     int64
}

type dashboardOptions struct {
	Suppliers []string `json:"suppliers"`
	Models    []string `json:"models"`
}

type dashboardResponse struct {
	Period      string                `json:"period"`
	RangeStart  time.Time             `json:"range_start"`
	RangeEnd    time.Time             `json:"range_end"`
	GeneratedAt time.Time             `json:"generated_at"`
	Summary     dashboardSummary      `json:"summary"`
	Series      []dashboardBucket     `json:"series"`
	Models      []dashboardModelUsage `json:"models"`
	Options     dashboardOptions      `json:"options"`
}

func (a *app) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query := dashboardQuery{
		Period:   normalizedDashboardPeriod(r.URL.Query().Get("period")),
		Supplier: strings.TrimSpace(r.URL.Query().Get("supplier")),
		Model:    strings.TrimSpace(r.URL.Query().Get("model")),
	}
	now := time.Now()
	start, end := dashboardPeriodRange(query.Period, now)
	if query.Period == "all" && a.db != nil {
		if response, err := buildDashboardAllResponseDB(a.db, query, now, start, end); err == nil {
			writeJSON(w, http.StatusOK, response)
			return
		}
	}
	logs, options, lifetimeTokens := a.dashboardData(start, end, query)
	if query.Period == "all" {
		start = dashboardAllPeriodStart(logs, query, now)
	}
	writeJSON(w, http.StatusOK, buildDashboardResponse(logs, query, options, now, start, end, lifetimeTokens))
}

func (a *app) dashboardData(start, end time.Time, query dashboardQuery) ([]requestLog, dashboardOptions, int64) {
	if a.db != nil {
		logs, logsErr := listRequestLogsRangeDB(a.db, start, end)
		options, optionsErr := dashboardFilterOptionsDB(a.db, query.Supplier)
		lifetimeTokens, totalErr := sumDashboardTokensDB(a.db, query)
		if logsErr == nil && optionsErr == nil && totalErr == nil {
			return logs, options, lifetimeTokens
		}
	}
	allLogs := a.currentLogs()
	logs := make([]requestLog, 0, len(allLogs))
	for _, log := range allLogs {
		if !log.At.Before(start) && log.At.Before(end) {
			logs = append(logs, log)
		}
	}
	return logs, dashboardFilterOptions(allLogs, query.Supplier), sumFilteredTokens(allLogs, query)
}

func normalizedDashboardPeriod(period string) string {
	switch period {
	case "day", "7d", "30d", "all":
		return period
	default:
		return "day"
	}
}

func dashboardPeriodRange(period string, now time.Time) (time.Time, time.Time) {
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	switch period {
	case "7d":
		return today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
	case "30d":
		return today.AddDate(0, 0, -29), today.AddDate(0, 0, 1)
	case "all":
		return time.Unix(0, 0).In(location), today.AddDate(0, 0, 1)
	default:
		return today, today.AddDate(0, 0, 1)
	}
}

func dashboardAllPeriodStart(logs []requestLog, query dashboardQuery, now time.Time) time.Time {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	found := false
	for _, log := range logs {
		if !dashboardLogMatches(log, query) {
			continue
		}
		local := log.At.In(now.Location())
		candidate := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, now.Location())
		if !found || candidate.Before(start) {
			start = candidate
			found = true
		}
	}
	return start
}

func buildDashboardResponse(logs []requestLog, query dashboardQuery, options dashboardOptions, now, start, end time.Time, lifetimeTokens int64) dashboardResponse {
	buckets := newDashboardBuckets(query.Period, start, end)
	modelUsage := map[string]*dashboardModelUsage{}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var summary dashboardSummary
	var firstTokenTotal, durationTotal, firstTokenCount int64

	for _, log := range logs {
		if !dashboardLogMatches(log, query) {
			continue
		}
		summary.InputTokens += log.InputTokens
		summary.OutputTokens += log.OutputTokens
		summary.CacheHitTokens += log.CacheHitTokens
		summary.PeriodTokens += log.TotalTokens
		summary.Requests++
		durationTotal += log.DurationMS
		if log.FirstTokenMS > 0 {
			firstTokenTotal += log.FirstTokenMS
			firstTokenCount++
		}
		if log.Status >= 400 {
			summary.Failures++
		}
		if !log.At.Before(todayStart) && log.At.Before(todayStart.AddDate(0, 0, 1)) {
			summary.TodayTokens += log.TotalTokens
		}
		supplier := dashboardSupplierName(log)
		model := dashboardModelName(log)
		seriesKey := supplier + "\x00" + model

		bucketIndex := dashboardBucketIndex(query.Period, start, log.At)
		if bucketIndex >= 0 && bucketIndex < len(buckets) {
			bucket := &buckets[bucketIndex]
			bucket.InputTokens += log.InputTokens
			bucket.OutputTokens += log.OutputTokens
			bucket.CacheHitTokens += log.CacheHitTokens
			bucket.TotalTokens += log.TotalTokens
			bucket.Requests++
			bucket.Models[seriesKey] += log.TotalTokens
		}

		usage := modelUsage[seriesKey]
		if usage == nil {
			usage = &dashboardModelUsage{SeriesKey: seriesKey, Model: model, Supplier: supplier}
			modelUsage[seriesKey] = usage
		}
		usage.InputTokens += log.InputTokens
		usage.OutputTokens += log.OutputTokens
		usage.CacheHitTokens += log.CacheHitTokens
		usage.TotalTokens += log.TotalTokens
		usage.Requests++
	}

	summary.LifetimeTokens = lifetimeTokens
	if summary.Requests > 0 {
		summary.AverageMS = float64(durationTotal) / float64(summary.Requests)
	}
	if firstTokenCount > 0 {
		summary.AverageFirstMS = float64(firstTokenTotal) / float64(firstTokenCount)
	}

	models := make([]dashboardModelUsage, 0, len(modelUsage))
	for _, usage := range modelUsage {
		models = append(models, *usage)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].TotalTokens == models[j].TotalTokens {
			return models[i].Requests > models[j].Requests
		}
		return models[i].TotalTokens > models[j].TotalTokens
	})

	return dashboardResponse{
		Period: query.Period, RangeStart: start, RangeEnd: end, GeneratedAt: now,
		Summary: summary, Series: buckets, Models: models, Options: options,
	}
}

func newDashboardBuckets(period string, start, end time.Time) []dashboardBucket {
	count := 24
	if period == "all" {
		count = 0
		for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 1, 0) {
			count++
		}
	} else if period != "day" {
		count = 0
		for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
			count++
		}
	}
	buckets := make([]dashboardBucket, count)
	for index := range buckets {
		bucketTime := start.Add(time.Duration(index) * time.Hour)
		label := bucketTime.Format("15:00")
		key := bucketTime.Format(time.RFC3339)
		if period == "all" {
			bucketTime = start.AddDate(0, index, 0)
			key = bucketTime.Format("2006-01")
			label = bucketTime.Format("2006/01")
		} else if period != "day" {
			bucketTime = start.AddDate(0, 0, index)
			key = bucketTime.Format("2006-01-02")
			label = bucketTime.Format("01/02")
		}
		buckets[index] = dashboardBucket{Key: key, Label: label, Models: map[string]int64{}}
	}
	return buckets
}

func dashboardBucketIndex(period string, start, at time.Time) int {
	local := at.In(start.Location())
	if period == "day" {
		return local.Hour()
	}
	if period == "all" {
		return (local.Year()-start.Year())*12 + int(local.Month()-start.Month())
	}
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	return int(date.Sub(startDate) / (24 * time.Hour))
}

func dashboardLogMatches(log requestLog, query dashboardQuery) bool {
	return (query.Supplier == "" || dashboardSupplierName(log) == query.Supplier) &&
		(query.Model == "" || dashboardModelName(log) == query.Model)
}

func dashboardSupplierName(log requestLog) string {
	if name := strings.TrimSpace(log.UpstreamName); name != "" {
		return name
	}
	if provider := strings.TrimSpace(log.UpstreamProvider); provider != "" {
		return provider
	}
	return "未标注供应商"
}

func dashboardModelName(log requestLog) string {
	if model := strings.TrimSpace(log.Model); model != "" {
		return model
	}
	return "未标注模型"
}

func dashboardFilterOptions(logs []requestLog, selectedSupplier string) dashboardOptions {
	supplierSet := map[string]bool{}
	modelSet := map[string]bool{}
	for _, log := range logs {
		supplier := dashboardSupplierName(log)
		supplierSet[supplier] = true
		if selectedSupplier == "" || selectedSupplier == supplier {
			modelSet[dashboardModelName(log)] = true
		}
	}
	return dashboardOptions{Suppliers: sortedDashboardKeys(supplierSet), Models: sortedDashboardKeys(modelSet)}
}

func sortedDashboardKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sumFilteredTokens(logs []requestLog, query dashboardQuery) int64 {
	var total int64
	for _, log := range logs {
		if dashboardLogMatches(log, query) {
			total += log.TotalTokens
		}
	}
	return total
}
func listRequestLogsRangeDB(db *sql.DB, start, end time.Time) ([]requestLog, error) {
	rows, err := db.Query(`
SELECT at, model, upstream_name, upstream_provider, status, duration_ms, first_token_ms,
       input_tokens, output_tokens, total_tokens, cache_hit_tokens
FROM request_logs
WHERE julianday(at) >= julianday(?) AND julianday(at) < julianday(?)
ORDER BY id ASC
`, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]requestLog, 0)
	for rows.Next() {
		var log requestLog
		var at string
		if err := rows.Scan(&at, &log.Model, &log.UpstreamName, &log.UpstreamProvider, &log.Status, &log.DurationMS, &log.FirstTokenMS,
			&log.InputTokens, &log.OutputTokens, &log.TotalTokens, &log.CacheHitTokens); err != nil {
			return nil, err
		}
		log.At, _ = time.Parse(time.RFC3339Nano, at)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func dashboardFilterOptionsDB(db *sql.DB, selectedSupplier string) (dashboardOptions, error) {
	rows, err := db.Query(`
SELECT DISTINCT upstream_name, upstream_provider, model
FROM request_logs
ORDER BY upstream_name, upstream_provider, model
`)
	if err != nil {
		return dashboardOptions{}, err
	}
	defer rows.Close()
	logs := make([]requestLog, 0)
	for rows.Next() {
		var log requestLog
		if err := rows.Scan(&log.UpstreamName, &log.UpstreamProvider, &log.Model); err != nil {
			return dashboardOptions{}, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return dashboardOptions{}, err
	}
	return dashboardFilterOptions(logs, selectedSupplier), nil
}

func sumDashboardTokensDB(db *sql.DB, query dashboardQuery) (int64, error) {
	const supplierSQL = "COALESCE(NULLIF(TRIM(upstream_name), ''), NULLIF(TRIM(upstream_provider), ''), '未标注供应商')"
	const modelSQL = "COALESCE(NULLIF(TRIM(model), ''), '未标注模型')"
	statement := "SELECT COALESCE(SUM(total_tokens), 0) FROM request_logs WHERE (? = '' OR " + supplierSQL + " = ?) AND (? = '' OR " + modelSQL + " = ?)"
	var total int64
	err := db.QueryRow(statement, query.Supplier, query.Supplier, query.Model, query.Model).Scan(&total)
	return total, err
}

func buildDashboardAllResponseDB(db *sql.DB, query dashboardQuery, now, start, end time.Time) (dashboardResponse, error) {
	aggregates, err := dashboardMonthlyAggregatesDB(db, query, now, start, end)
	if err != nil {
		return dashboardResponse{}, err
	}
	options, err := dashboardFilterOptionsDB(db, query.Supplier)
	if err != nil {
		return dashboardResponse{}, err
	}

	rangeStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	if len(aggregates) > 0 {
		if parsed, parseErr := time.ParseInLocation("2006-01", aggregates[0].Month, now.Location()); parseErr == nil {
			rangeStart = parsed
		}
	}
	buckets := newDashboardBuckets("all", rangeStart, end)
	bucketIndexes := make(map[string]int, len(buckets))
	for index, bucket := range buckets {
		bucketIndexes[bucket.Key] = index
	}

	modelUsage := map[string]*dashboardModelUsage{}
	var summary dashboardSummary
	var durationTotal, firstTokenTotal, firstTokenCount int64
	for _, aggregate := range aggregates {
		summary.InputTokens += aggregate.InputTokens
		summary.OutputTokens += aggregate.OutputTokens
		summary.CacheHitTokens += aggregate.CacheHitTokens
		summary.PeriodTokens += aggregate.TotalTokens
		summary.TodayTokens += aggregate.TodayTokens
		summary.Requests += aggregate.Requests
		summary.Failures += aggregate.Failures
		durationTotal += aggregate.DurationTotal
		firstTokenTotal += aggregate.FirstTokenTotal
		firstTokenCount += aggregate.FirstTokenCount

		seriesKey := aggregate.Supplier + "\x00" + aggregate.Model
		if bucketIndex, ok := bucketIndexes[aggregate.Month]; ok {
			bucket := &buckets[bucketIndex]
			bucket.InputTokens += aggregate.InputTokens
			bucket.OutputTokens += aggregate.OutputTokens
			bucket.CacheHitTokens += aggregate.CacheHitTokens
			bucket.TotalTokens += aggregate.TotalTokens
			bucket.Requests += aggregate.Requests
			bucket.Models[seriesKey] += aggregate.TotalTokens
		}

		usage := modelUsage[seriesKey]
		if usage == nil {
			usage = &dashboardModelUsage{SeriesKey: seriesKey, Model: aggregate.Model, Supplier: aggregate.Supplier}
			modelUsage[seriesKey] = usage
		}
		usage.InputTokens += aggregate.InputTokens
		usage.OutputTokens += aggregate.OutputTokens
		usage.CacheHitTokens += aggregate.CacheHitTokens
		usage.TotalTokens += aggregate.TotalTokens
		usage.Requests += aggregate.Requests
	}

	summary.LifetimeTokens = summary.PeriodTokens
	if summary.Requests > 0 {
		summary.AverageMS = float64(durationTotal) / float64(summary.Requests)
	}
	if firstTokenCount > 0 {
		summary.AverageFirstMS = float64(firstTokenTotal) / float64(firstTokenCount)
	}

	models := make([]dashboardModelUsage, 0, len(modelUsage))
	for _, usage := range modelUsage {
		models = append(models, *usage)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].TotalTokens == models[j].TotalTokens {
			return models[i].Requests > models[j].Requests
		}
		return models[i].TotalTokens > models[j].TotalTokens
	})

	return dashboardResponse{
		Period: "all", RangeStart: rangeStart, RangeEnd: end, GeneratedAt: now,
		Summary: summary, Series: buckets, Models: models, Options: options,
	}, nil
}

func dashboardMonthlyAggregatesDB(db *sql.DB, query dashboardQuery, now, start, end time.Time) ([]dashboardMonthlyAggregate, error) {
	const supplierSQL = "COALESCE(NULLIF(TRIM(upstream_name), ''), NULLIF(TRIM(upstream_provider), ''), '未标注供应商')"
	const modelSQL = "COALESCE(NULLIF(TRIM(model), ''), '未标注模型')"
	statement := `
SELECT strftime('%Y-%m', at, 'localtime') AS month,
       ` + supplierSQL + ` AS supplier,
       ` + modelSQL + ` AS model,
       COALESCE(SUM(input_tokens), 0),
       COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(cache_hit_tokens), 0),
       COALESCE(SUM(total_tokens), 0),
       COUNT(*),
       COALESCE(SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(duration_ms), 0),
       COALESCE(SUM(CASE WHEN first_token_ms > 0 THEN first_token_ms ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN first_token_ms > 0 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE
           WHEN julianday(at) >= julianday(?) AND julianday(at) < julianday(?) THEN total_tokens
           ELSE 0
       END), 0)
FROM request_logs
WHERE julianday(at) >= julianday(?) AND julianday(at) < julianday(?)
  AND (? = '' OR ` + supplierSQL + ` = ?)
  AND (? = '' OR ` + modelSQL + ` = ?)
GROUP BY month, supplier, model
ORDER BY month, supplier, model
`
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	rows, err := db.Query(
		statement,
		todayStart.Format(time.RFC3339Nano),
		todayStart.AddDate(0, 0, 1).Format(time.RFC3339Nano),
		start.Format(time.RFC3339Nano),
		end.Format(time.RFC3339Nano),
		query.Supplier,
		query.Supplier,
		query.Model,
		query.Model,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aggregates := make([]dashboardMonthlyAggregate, 0)
	for rows.Next() {
		var aggregate dashboardMonthlyAggregate
		if err := rows.Scan(
			&aggregate.Month,
			&aggregate.Supplier,
			&aggregate.Model,
			&aggregate.InputTokens,
			&aggregate.OutputTokens,
			&aggregate.CacheHitTokens,
			&aggregate.TotalTokens,
			&aggregate.Requests,
			&aggregate.Failures,
			&aggregate.DurationTotal,
			&aggregate.FirstTokenTotal,
			&aggregate.FirstTokenCount,
			&aggregate.TodayTokens,
		); err != nil {
			return nil, err
		}
		aggregates = append(aggregates, aggregate)
	}
	return aggregates, rows.Err()
}
