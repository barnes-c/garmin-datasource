package plugin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/barnesc/garminconnect/pkg/models"
)

func fieldByName(t *testing.T, frame *data.Frame, name string) *data.Field {
	t.Helper()
	for _, f := range frame.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("frame %q has no field %q", frame.Name, name)
	return nil
}

func timeRange(days int) backend.TimeRange {
	to := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	return backend.TimeRange{From: to.AddDate(0, 0, -days), To: to}
}

func TestQueryActivities(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/activitylist-service/activities/search/activities": activitiesFixture,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryActivities(context.Background(), queryModel{}, timeRange(7))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 activities, got %d", frame.Rows())
	}

	speed := fieldByName(t, frame, "average_speed")
	if got := speed.At(0).(float64); math.Abs(got-8.333*3.6) > 0.01 {
		t.Errorf("expected km/h conversion, got %v", got)
	}
	if speed.Config.Unit != "velocitykmh" {
		t.Errorf("expected velocitykmh unit, got %q", speed.Config.Unit)
	}

	// end_time uses elapsed duration, falling back to duration.
	start := fieldByName(t, frame, "time").At(0).(time.Time)
	end := fieldByName(t, frame, "end_time").At(0).(time.Time)
	if end.Sub(start) != 3700*time.Second {
		t.Errorf("expected elapsed-based end_time, got %v", end.Sub(start))
	}
	start = fieldByName(t, frame, "time").At(1).(time.Time)
	end = fieldByName(t, frame, "end_time").At(1).(time.Time)
	if end.Sub(start) != 1800*time.Second {
		t.Errorf("expected duration fallback end_time, got %v", end.Sub(start))
	}

	// Coordinates are nullable: set for outdoor, nil for indoor.
	lat := fieldByName(t, frame, "lat")
	if got := lat.At(0).(*float64); got == nil || *got != 46.2409 {
		t.Errorf("expected start latitude 46.2409, got %v", got)
	}
	if got := lat.At(1).(*float64); got != nil {
		t.Errorf("expected nil latitude for activity without coordinates, got %v", *got)
	}
	endLat := fieldByName(t, frame, "end_lat")
	if got := endLat.At(0).(*float64); got == nil || *got != 46.2427 {
		t.Errorf("expected end latitude 46.2427, got %v", got)
	}

	// Second call is served from cache.
	if resp := d.queryActivities(context.Background(), queryModel{}, timeRange(7)); resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if got := f.count("/activitylist-service/activities/search/activities"); got != 1 {
		t.Errorf("expected 1 upstream request, got %d", got)
	}
}

func TestQueryActivitiesSpeedUnits(t *testing.T) {
	for unit, want := range map[string]float64{"ms": 8.333, "mph": 8.333 * 2.236936, "kmh": 8.333 * 3.6} {
		f := newFixtureServer(t, map[string]string{
			"/activitylist-service/activities/search/activities": activitiesFixture,
		})
		d := newTestDatasource(f, models.PluginSettings{SpeedUnit: unit})
		resp := d.queryActivities(context.Background(), queryModel{}, timeRange(7))
		if resp.Error != nil {
			t.Fatal(resp.Error)
		}
		got := fieldByName(t, resp.Frames[0], "average_speed").At(0).(float64)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("%s: expected %v, got %v", unit, want, got)
		}
	}
}

func TestQueryActivitiesImperial(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/activitylist-service/activities/search/activities": activitiesFixture,
	})
	d := newTestDatasource(f, models.PluginSettings{UnitSystem: "imperial"})

	resp := d.queryActivities(context.Background(), queryModel{}, timeRange(7))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]

	dist := fieldByName(t, frame, "distance")
	if got := dist.At(0).(float64); math.Abs(got-30000/1609.344) > 0.01 {
		t.Errorf("expected miles conversion, got %v", got)
	}
	if dist.Config.Unit != "lengthmi" {
		t.Errorf("expected lengthmi unit, got %q", dist.Config.Unit)
	}

	elev := fieldByName(t, frame, "elevation_gain")
	if got := elev.At(0).(float64); math.Abs(got-250*3.280839895) > 0.1 {
		t.Errorf("expected feet conversion, got %v", got)
	}
	if elev.Config.Unit != "lengthft" {
		t.Errorf("expected lengthft unit, got %q", elev.Config.Unit)
	}
}

func TestQueryTrack(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/download-service/export/gpx/activity/42": trackGPXFixture,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryTrack(context.Background(), queryModel{ActivityID: "42"})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 3 {
		t.Fatalf("expected 3 trackpoints, got %d", frame.Rows())
	}

	// Two ~111m steps north; distance accumulates, speed is per-step km/h.
	dist := fieldByName(t, frame, "distance")
	total := dist.At(2).(float64)
	if total < 200 || total > 250 {
		t.Errorf("expected ~222m cumulative distance, got %v", total)
	}
	speed := fieldByName(t, frame, "speed").At(1).(*float64)
	if speed == nil || *speed < 35 || *speed > 45 {
		t.Errorf("expected ~40 km/h step speed, got %v", speed)
	}

	// Cached on repeat.
	d.queryTrack(context.Background(), queryModel{ActivityID: "42"})
	if got := f.count("/download-service/export/gpx/activity/42"); got != 1 {
		t.Errorf("expected 1 download, got %d", got)
	}
}

func TestQuerySplitsLapFallback(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/activity-service/activity/1/splits": splitsWithLapsFixture,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryTable(context.Background(), queryTypeSplits, queryModel{ActivityID: "1"})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 laps from lapDTOs, got %d rows", frame.Rows())
	}
	if got := fieldByName(t, frame, "average_speed").At(0).(float64); math.Abs(got-7.1*3.6) > 0.01 {
		t.Errorf("expected converted lap speed, got %v", got)
	}
}

func TestStepsChunking(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/usersummary-service/stats/steps/daily/": `[{"calendarDate": "2026-06-01", "totalSteps": 1000}]`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "steps"}, timeRange(40))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if got := f.count("/usersummary-service/stats/steps/daily/"); got != 2 {
		t.Errorf("expected 40-day range split into 2 requests, got %d", got)
	}
}

func TestQuerySportTotals(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/activitylist-service/activities/search/activities": activitiesFixture,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.querySportTotals(context.Background(), queryModel{}, timeRange(7))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 sports, got %d", frame.Rows())
	}
	// Sorted by distance descending: cycling (30 km) before running (5 km).
	if got := fieldByName(t, frame, "sport").At(0).(string); got != "cycling" {
		t.Errorf("expected cycling first, got %q", got)
	}
	dist := fieldByName(t, frame, "distance")
	if got := dist.At(0).(float64); got != 30000 {
		t.Errorf("expected 30000 m cycling total, got %v", got)
	}
	if dist.Config.Unit != "lengthm" {
		t.Errorf("expected lengthm unit, got %q", dist.Config.Unit)
	}
	if got := fieldByName(t, frame, "activities").At(1).(int64); got != 1 {
		t.Errorf("expected 1 running activity, got %d", got)
	}

	// Derived from the activities query: a following activities query is
	// served from the same cache, with one API call in total.
	if resp := d.queryActivities(context.Background(), queryModel{}, timeRange(7)); resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if got := f.count("/activitylist-service/activities/search/activities"); got != 1 {
		t.Errorf("expected sport totals to share the activities fetch, got %d API calls", got)
	}
}

func TestLactateThresholdSortedAscending(t *testing.T) {
	// The endpoint returns a list with no order guarantee; frame must be
	// ascending so lastNotNull reductions pick the newest measurement.
	f := newFixtureServer(t, map[string]string{
		"/biometric-service/biometric/latestLactateThreshold": `[
			{"calendarDate": "2026-07-01", "speed": 3.2, "hearRate": 177},
			{"calendarDate": "2026-05-01", "speed": 3.0, "hearRate": 171}
		]`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "lactate_threshold"}, timeRange(90))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	hr := fieldByName(t, resp.Frames[0], "hr_running")
	if got := hr.At(1).(*float64); got == nil || *got != 177 {
		t.Errorf("expected newest measurement (177) last, got %v", got)
	}
}

func TestScoreChunking(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/metrics-service/metrics/endurancescore/stats": `{"groupMap": {"2026-06-01": {"groupAverage": 5000}}}`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "endurance_score"}, timeRange(730))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if got := f.count("/metrics-service/metrics/endurancescore/stats"); got != 3 {
		t.Errorf("expected 2-year range split into 3 requests, got %d", got)
	}
}

func TestBodyBatteryChunking(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/wellness-service/wellness/bodyBattery/reports/daily": `[{"date": "2026-06-01", "bodyBatteryValuesArray": [[1780300800000, 80]]}]`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "body_battery"}, timeRange(40))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if got := f.count("/wellness-service/wellness/bodyBattery/reports/daily"); got != 2 {
		t.Errorf("expected 40-day range split into 2 requests, got %d", got)
	}
}

func TestFTPFallsBackToPowerZones(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/biometric-service/biometric/latestFunctionalThresholdPower/CYCLING": `{"functionalThresholdPower": null}`,
		"/biometric-service/powerZones/sports/all":                            `[{"sport": "CYCLING", "functionalThresholdPower": 200}]`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "ftp"}, timeRange(30))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 1 || fieldByName(t, frame, "ftp").At(0).(float64) != 200 {
		t.Fatalf("expected FTP 200 from power zone fallback, got %d rows", frame.Rows())
	}
	if frame.Meta == nil || len(frame.Meta.Notices) != 1 {
		t.Error("expected an info notice about the fallback")
	}
}

func TestWeightUsesDateRangeEndpoint(t *testing.T) {
	// Real dateRange payload shape: date is epoch ms, weight in grams,
	// composition fields null for manual entries.
	f := newFixtureServer(t, map[string]string{
		"/weight-service/weight/dateRange": `{"startDate": "2026-06-05", "endDate": "2026-07-05", "dateWeightList": [
			{"calendarDate": "2026-07-04", "weight": 76800.0, "bmi": null, "bodyFat": null, "date": 1783126590009, "sourceType": "MANUAL"},
			{"calendarDate": "2026-06-20", "weight": 76000.0, "bmi": 23.1, "bodyFat": 18.5, "date": 1781952319006, "sourceType": "INDEX_SCALE"}
		]}`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "weight"}, timeRange(30))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 weigh-ins, got %d", frame.Rows())
	}
	// The API returns newest-first; the frame must be ascending so that
	// reductions like lastNotNull pick the most recent weigh-in.
	weight := fieldByName(t, frame, "weight")
	if got := weight.At(0).(float64); got != 76.0 {
		t.Errorf("expected oldest weigh-in (76.0 kg) first, got %v", got)
	}
	if got := weight.At(1).(float64); got != 76.8 {
		t.Errorf("expected newest weigh-in (76.8 kg) last, got %v", got)
	}

	comp := d.queryMetric(context.Background(), queryModel{Metric: "body_composition"}, timeRange(30))
	if comp.Error != nil {
		t.Fatal(comp.Error)
	}
	bmi := fieldByName(t, comp.Frames[0], "bmi")
	if v := bmi.At(0).(*float64); v == nil || *v != 23.1 {
		t.Errorf("expected bmi 23.1 first (ascending time), got %v", v)
	}
	if bmi.At(1).(*float64) != nil {
		t.Error("null bmi must map to nil")
	}
}

func TestIntensityMinutesFromDailySummary(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/usersummary-service/usersummary/daily/": `{"calendarDate": "2026-07-02", "moderateIntensityMinutes": 40, "vigorousIntensityMinutes": 81}`,
	})
	d := newTestDatasource(f, models.PluginSettings{})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "intensity_minutes"}, timeRange(2))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	frame := resp.Frames[0]
	if frame.Rows() == 0 {
		t.Fatal("expected intensity minutes points")
	}
	// Garmin counts vigorous minutes double: 40 + 2*81 = 202.
	if got := fieldByName(t, frame, "intensity_minutes").At(0).(float64); got != 202 {
		t.Errorf("expected 202 intensity minutes, got %v", got)
	}
}

func TestWeightImperial(t *testing.T) {
	f := newFixtureServer(t, map[string]string{
		"/weight-service/weight/dateRange": `{"dateWeightList": [{"calendarDate": "2026-07-04", "weight": 76800.0}]}`,
	})
	d := newTestDatasource(f, models.PluginSettings{UnitSystem: "imperial"})

	resp := d.queryMetric(context.Background(), queryModel{Metric: "weight"}, timeRange(30))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	weight := fieldByName(t, resp.Frames[0], "weight")
	if got := weight.At(0).(float64); math.Abs(got-76.8*2.20462262185) > 0.01 {
		t.Errorf("expected pounds conversion, got %v", got)
	}
	if weight.Config.Unit != "masslbs" {
		t.Errorf("expected masslbs unit, got %q", weight.Config.Unit)
	}
}

func TestPerDayPartialFailure(t *testing.T) {
	var calls atomic.Int64
	get := func(_ *garminconnect.Client, _ context.Context, d time.Time) (*int, error) {
		calls.Add(1)
		if d.Day() == 3 {
			return nil, errors.New("boom")
		}
		v := d.Day()
		return &v, nil
	}
	fetch := perDayValue("test", get, func(v *int) (float64, bool) { return float64(*v), true })

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	points, notices, err := fetch(context.Background(), nil, nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 4 {
		t.Errorf("expected 4 points (1 day failed), got %d", len(points))
	}
	if len(notices) != 1 {
		t.Fatalf("expected a partial-failure notice, got %d", len(notices))
	}
	if calls.Load() != 5 {
		t.Errorf("expected 5 day calls, got %d", calls.Load())
	}
}

func TestPerDayRangeClamps(t *testing.T) {
	var calls atomic.Int64
	get := func(_ *garminconnect.Client, _ context.Context, d time.Time) (*int, error) {
		calls.Add(1)
		v := d.Day()
		return &v, nil
	}
	fetch := perDayValue("test", get, func(v *int) (float64, bool) { return float64(*v), true })

	points, notices, err := fetch(context.Background(), nil, nil, time.Now().AddDate(0, 0, -200), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != maxMetricDays {
		t.Errorf("expected range clamped to %d day calls, got %d", maxMetricDays, calls.Load())
	}
	if len(points) != maxMetricDays {
		t.Errorf("expected %d points, got %d", maxMetricDays, len(points))
	}
	if len(notices) != 1 || notices[0].Severity != data.NoticeSeverityWarning {
		t.Fatalf("expected a clamp warning notice, got %v", notices)
	}
}

func TestChunkedPartialFailure(t *testing.T) {
	calls := 0
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 60) // 3 chunks of 28 days
	got, failed, total, firstErr := chunked(from, to, 28, func(_, _ time.Time) ([]int, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("boom")
		}
		return []int{calls}, nil
	})
	if total != 3 || failed != 1 || firstErr == nil {
		t.Fatalf("expected 3 chunks with 1 failure, got total=%d failed=%d err=%v", total, failed, firstErr)
	}
	if len(got) != 2 {
		t.Errorf("expected results from the 2 successful chunks, got %v", got)
	}
}

func TestLoginBackoff(t *testing.T) {
	d := &Datasource{
		settings:   &models.PluginSettings{Email: "a@b.c", Secrets: &models.SecretPluginSettings{Password: "x"}},
		frameCache: map[string]cachedFrame{},
	}

	a := &loginAttempt{err: fmt.Errorf("bad credentials"), done: make(chan struct{})}
	close(a.done)
	d.login = a
	if _, err := d.finishLogin(a); err == nil {
		t.Fatal("expected login error")
	}

	if _, err := d.garminClient(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "next attempt allowed") {
		t.Fatalf("expected backoff error, got %v", err)
	}

	// Success resets the backoff.
	ok := &loginAttempt{done: make(chan struct{})}
	close(ok.done)
	d.mu.Lock()
	d.login = ok
	d.mu.Unlock()
	if _, err := d.finishLogin(ok); err != nil {
		t.Fatal(err)
	}
	if d.loginFailures != 0 || !d.loginBlockedUntil.IsZero() {
		t.Error("expected backoff reset after success")
	}
}

func TestCacheEviction(t *testing.T) {
	d := &Datasource{frameCache: map[string]cachedFrame{}}
	for i := 0; i < maxCachedFrames; i++ {
		d.cachePut(fmt.Sprintf("k%d", i), data.NewFrame("f"), time.Duration(i+1)*time.Minute)
	}
	d.cachePut("overflow", data.NewFrame("f"), time.Hour)
	if len(d.frameCache) > maxCachedFrames-maxCachedFrames/4+1 {
		t.Errorf("expected eviction of ~quarter, cache has %d entries", len(d.frameCache))
	}
	if _, ok := d.cacheGet("overflow"); !ok {
		t.Error("newly inserted entry must survive eviction")
	}
	// Entries closest to expiry are evicted first.
	if _, ok := d.cacheGet("k0"); ok {
		t.Error("oldest-expiry entry should have been evicted")
	}
}
