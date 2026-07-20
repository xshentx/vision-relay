package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildDashboardResponseAggregatesBySupplierAndModel(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, location)
	start, end := dashboardPeriodRange("7d", now)
	logs := []requestLog{
		{At: time.Date(2026, time.July, 20, 9, 0, 0, 0, location), UpstreamName: "GPT", Model: "gpt-5.6", Status: 200, DurationMS: 1000, FirstTokenMS: 200, InputTokens: 70, OutputTokens: 30, TotalTokens: 100, CacheHitTokens: 20},
		{At: time.Date(2026, time.July, 19, 9, 0, 0, 0, location), UpstreamName: "GPT", Model: "gpt-5.5", Status: 502, DurationMS: 500, InputTokens: 40, OutputTokens: 10, TotalTokens: 50},
		{At: time.Date(2026, time.July, 20, 10, 0, 0, 0, location), UpstreamName: "Claude", Model: "claude-opus", Status: 200, TotalTokens: 999},
	}
	query := dashboardQuery{Period: "7d", Supplier: "GPT"}
	response := buildDashboardResponse(logs, query, dashboardFilterOptions(logs, query.Supplier), now, start, end, 150)

	if response.Summary.LifetimeTokens != 150 || response.Summary.TodayTokens != 100 || response.Summary.PeriodTokens != 150 {
		t.Fatalf("unexpected token summary: %#v", response.Summary)
	}
	if response.Summary.InputTokens != 110 || response.Summary.OutputTokens != 40 || response.Summary.CacheHitTokens != 20 {
		t.Fatalf("unexpected token breakdown: %#v", response.Summary)
	}
	if response.Summary.Requests != 2 || response.Summary.Failures != 1 || response.Summary.AverageMS != 750 || response.Summary.AverageFirstMS != 200 {
		t.Fatalf("unexpected request summary: %#v", response.Summary)
	}
	if len(response.Models) != 2 || response.Models[0].Model != "gpt-5.6" || response.Models[0].TotalTokens != 100 {
		t.Fatalf("unexpected model ranking: %#v", response.Models)
	}
	if len(response.Series) != 7 || response.Series[6].TotalTokens != 100 || response.Series[5].TotalTokens != 50 {
		t.Fatalf("unexpected dashboard series: %#v", response.Series)
	}
	if len(response.Options.Models) != 2 || len(response.Options.Suppliers) != 2 {
		t.Fatalf("unexpected dashboard options: %#v", response.Options)
	}
}

func TestDashboardSeriesSeparatesSameModelAcrossSuppliers(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, location)
	start, end := dashboardPeriodRange("day", now)
	logs := []requestLog{
		{At: time.Date(2026, time.July, 20, 9, 10, 0, 0, location), UpstreamName: "Supplier A", Model: "shared-model", Status: 200, TotalTokens: 10},
		{At: time.Date(2026, time.July, 20, 9, 20, 0, 0, location), UpstreamName: "Supplier B", Model: "shared-model", Status: 200, TotalTokens: 20},
	}

	response := buildDashboardResponse(logs, dashboardQuery{Period: "day"}, dashboardFilterOptions(logs, ""), now, start, end, 30)
	if len(response.Models) != 2 {
		t.Fatalf("model ranking length = %d, want 2", len(response.Models))
	}
	seriesKeys := map[string]string{}
	for _, model := range response.Models {
		if model.SeriesKey == "" {
			t.Fatalf("series key is empty for %#v", model)
		}
		seriesKeys[model.Supplier] = model.SeriesKey
	}
	if seriesKeys["Supplier A"] == seriesKeys["Supplier B"] {
		t.Fatalf("suppliers share a series key: %#v", seriesKeys)
	}
	bucket := response.Series[9]
	if bucket.Models[seriesKeys["Supplier A"]] != 10 || bucket.Models[seriesKeys["Supplier B"]] != 20 {
		t.Fatalf("same-name model series were merged: %#v", bucket.Models)
	}
}

func TestDashboardPeriodRangeUsesCalendarBoundaries(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 12, 30, 0, 0, location)
	start, end := dashboardPeriodRange("30d", now)
	if start != time.Date(2026, time.June, 21, 0, 0, 0, 0, location) ||
		end != time.Date(2026, time.July, 21, 0, 0, 0, 0, location) {
		t.Fatalf("unexpected 30-day range: %s - %s", start, end)
	}
	if got := len(newDashboardBuckets("30d", start, end)); got != 30 {
		t.Fatalf("30-day bucket count = %d, want 30", got)
	}
}

func TestDashboardAllPeriodUsesMonthlyBucketsFromFirstMatchingLog(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, time.July, 20, 12, 30, 0, 0, location)
	logs := []requestLog{
		{At: time.Date(2025, time.December, 3, 9, 0, 0, 0, location), UpstreamName: "Other"},
		{At: time.Date(2026, time.January, 15, 9, 0, 0, 0, location), UpstreamName: "GPT"},
		{At: time.Date(2026, time.July, 20, 9, 0, 0, 0, location), UpstreamName: "GPT"},
	}
	query := dashboardQuery{Period: "all", Supplier: "GPT"}
	start := dashboardAllPeriodStart(logs, query, now)
	_, end := dashboardPeriodRange("all", now)
	if start != time.Date(2026, time.January, 1, 0, 0, 0, 0, location) {
		t.Fatalf("all-period start = %s, want 2026-01-01", start)
	}
	buckets := newDashboardBuckets("all", start, end)
	if len(buckets) != 7 || buckets[0].Key != "2026-01" || buckets[6].Key != "2026-07" {
		t.Fatalf("unexpected all-period buckets: %#v", buckets)
	}
	if got := dashboardBucketIndex("all", start, logs[2].At); got != 6 {
		t.Fatalf("all-period bucket index = %d, want 6", got)
	}
}

func TestNormalizedDashboardPeriodSupportsCurrentOptions(t *testing.T) {
	for _, period := range []string{"day", "7d", "30d", "all"} {
		if got := normalizedDashboardPeriod(period); got != period {
			t.Fatalf("normalized period = %q, want %q", got, period)
		}
	}
	if got := normalizedDashboardPeriod("month"); got != "day" {
		t.Fatalf("legacy month period = %q, want day fallback", got)
	}
}

func TestDashboardBucketIndexUsesCalendarDaysAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.March, 7, 0, 0, 0, 0, location)
	at := time.Date(2026, time.March, 9, 12, 0, 0, 0, location)
	if got := dashboardBucketIndex("7d", start, at); got != 2 {
		t.Fatalf("bucket index across DST = %d, want 2", got)
	}
}

func TestHandleDashboardReturnsJSONAndRejectsWrites(t *testing.T) {
	now := time.Now()
	a := &app{logs: []requestLog{{At: now, UpstreamName: "GPT", Model: "gpt-5.6", Status: 200, InputTokens: 4, OutputTokens: 2, TotalTokens: 6}}}
	recorder := httptest.NewRecorder()
	a.handleDashboard(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard?period=day", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", recorder.Code)
	}
	var response dashboardResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Summary.PeriodTokens != 6 || len(response.Series) != 24 {
		t.Fatalf("unexpected response: %#v", response)
	}

	recorder = httptest.NewRecorder()
	a.handleDashboard(recorder, httptest.NewRequest(http.MethodPost, "/api/dashboard", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", recorder.Code)
	}
}

func TestDashboardDataUsesSQLiteRangeAndLifetimeQueries(t *testing.T) {
	db, err := openAppDB(filepath.Join(t.TempDir(), "dashboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	for _, log := range []requestLog{
		{At: now.Add(-time.Minute), UpstreamName: "GPT", Model: "current", Status: 200, InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
		{At: now.AddDate(0, 0, -10), UpstreamName: "GPT", Model: "older", Status: 200, InputTokens: 3, OutputTokens: 1, TotalTokens: 4},
	} {
		if err := insertRequestLogDB(db, log); err != nil {
			t.Fatal(err)
		}
	}
	query := dashboardQuery{Period: "day", Supplier: "GPT"}
	start, end := dashboardPeriodRange(query.Period, now)
	a := &app{db: db}
	logs, options, lifetimeTokens := a.dashboardData(start, end, query)
	if len(logs) != 1 || logs[0].Model != "current" {
		t.Fatalf("unexpected range logs: %#v", logs)
	}
	if lifetimeTokens != 10 {
		t.Fatalf("lifetime tokens = %d, want 10", lifetimeTokens)
	}
	if len(options.Models) != 2 || len(options.Suppliers) != 1 {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestBuildDashboardAllResponseDBAggregatesWithoutLoadingRawLogs(t *testing.T) {
	db, err := openAppDB(filepath.Join(t.TempDir(), "dashboard-all.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	previousMonthStart := currentMonthStart.AddDate(0, -1, 0)
	for _, log := range []requestLog{
		{At: previousMonthStart.Add(12 * time.Hour), UpstreamName: "GPT", Model: "gpt-5.6", Status: 502, DurationMS: 500, InputTokens: 40, OutputTokens: 10, TotalTokens: 50},
		{At: todayStart.Add(12 * time.Hour), UpstreamName: "GPT", Model: "gpt-5.6", Status: 200, DurationMS: 1000, FirstTokenMS: 200, InputTokens: 70, OutputTokens: 30, TotalTokens: 100, CacheHitTokens: 20},
		{At: todayStart.Add(13 * time.Hour), UpstreamName: "Claude", Model: "claude-opus", Status: 200, TotalTokens: 999},
	} {
		if err := insertRequestLogDB(db, log); err != nil {
			t.Fatal(err)
		}
	}

	query := dashboardQuery{Period: "all", Supplier: "GPT"}
	start, end := dashboardPeriodRange(query.Period, now)
	response, err := buildDashboardAllResponseDB(db, query, now, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if response.Summary.LifetimeTokens != 150 || response.Summary.PeriodTokens != 150 || response.Summary.TodayTokens != 100 {
		t.Fatalf("unexpected token summary: %#v", response.Summary)
	}
	if response.Summary.Requests != 2 || response.Summary.Failures != 1 || response.Summary.AverageMS != 750 || response.Summary.AverageFirstMS != 200 {
		t.Fatalf("unexpected request summary: %#v", response.Summary)
	}
	if len(response.Series) != 2 || response.Series[0].TotalTokens != 50 || response.Series[1].TotalTokens != 100 {
		t.Fatalf("unexpected monthly series: %#v", response.Series)
	}
	if len(response.Models) != 1 || response.Models[0].TotalTokens != 150 || response.Models[0].Requests != 2 {
		t.Fatalf("unexpected model usage: %#v", response.Models)
	}
	if len(response.Options.Suppliers) != 2 || len(response.Options.Models) != 1 {
		t.Fatalf("unexpected filter options: %#v", response.Options)
	}
}
