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
	start, end := dashboardPeriodRange("month", now)
	if start.Day() != 1 || start.Month() != time.July || end.Day() != 1 || end.Month() != time.August {
		t.Fatalf("unexpected month range: %s - %s", start, end)
	}
	if got := len(newDashboardBuckets("month", start, end)); got != 31 {
		t.Fatalf("month bucket count = %d, want 31", got)
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
		{At: now.Add(-time.Hour), UpstreamName: "GPT", Model: "current", Status: 200, InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
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
