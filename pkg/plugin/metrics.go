package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.opentelemetry.io/otel/attribute"
)

// maxMetricDays caps per-day metrics, which need one Garmin API call per day
// in the queried range.
const maxMetricDays = 93

type metricPoint struct {
	t time.Time
	v float64
}

// metricFetch fetchers receive the Datasource for display preferences (e.g.
// the configured speed unit) and return notices for partial results.
type metricFetch func(ctx context.Context, d *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error)

type frameFetch func(ctx context.Context, d *Datasource, client *garminconnect.Client, from, to time.Time) (*data.Frame, []data.Notice, error)

// metricDef describes one selectable metric. Single-series metrics set fetch
// and optionally unit; metrics with several related series (sleep stages,
// body composition, ...) set fetchFrame instead and build their own frame.
type metricDef struct {
	unit       string
	fetch      metricFetch
	fetchFrame frameFetch
}

var metricDefs = map[string]metricDef{
	"hydration": {unit: "mlitre", fetch: perDayValue("hydration", (*garminconnect.Client).Hydration,
		func(h *garminconnect.HydrationData) (float64, bool) { return h.ValueInML, h.ValueInML > 0 })},
	"ftp":                {unit: "watt", fetch: fetchFTP},
	"steps":              {fetch: fetchSteps},
	"resting_heart_rate": {fetch: fetchRestingHeartRate},
	"weight":             {unit: "masskg", fetch: fetchWeight},
	"vo2max":             {fetch: fetchVO2Max},
	"body_battery":       {fetch: fetchBodyBattery},

	"stress": {fetch: perDayValue("stress", (*garminconnect.Client).AllDayStress,
		func(s *garminconnect.StressData) (float64, bool) {
			return float64(s.AvgStressLevel), s.AvgStressLevel > 0
		})},
	"hrv": {fetch: perDayValue("hrv", (*garminconnect.Client).HRVData,
		func(h *garminconnect.HRVData) (float64, bool) {
			return float64(h.HRVSummary.LastNight), h.HRVSummary.LastNight > 0
		})},
	"spo2": {unit: "percent", fetch: perDayValue("spo2", (*garminconnect.Client).SpO2,
		func(s *garminconnect.SpO2Data) (float64, bool) { return s.AverageSpO2, s.AverageSpO2 > 0 })},
	"respiration": {fetch: perDayValue("respiration", (*garminconnect.Client).Respiration,
		func(r *garminconnect.RespirationData) (float64, bool) {
			return r.TodayAvgWakingRespirationValue, r.TodayAvgWakingRespirationValue > 0
		})},
	// The dedicated daily/im endpoint only carries week-to-date fields (and
	// the client models it wrong anyway); the daily summary has true per-day
	// values. Vigorous minutes count double, matching Garmin's own totals.
	"intensity_minutes": {unit: "m", fetch: perDayValue("intensity_minutes", (*garminconnect.Client).UserSummary,
		func(s *garminconnect.UserSummary) (float64, bool) {
			total := s.ModerateDurationMinutes + 2*s.VigorousDurationMinutes
			return float64(total), total > 0
		})},
	"training_readiness": {fetch: perDayValue("training_readiness", firstTrainingReadiness,
		func(t *garminconnect.TrainingReadiness) (float64, bool) { return float64(t.Score), t.Score > 0 })},
	"fitness_age": {fetch: perDayValue("fitness_age", (*garminconnect.Client).FitnessAge,
		func(f *garminconnect.FitnessAge) (float64, bool) { return f.FitnessAge, f.FitnessAge > 0 })},
	"floors": {fetch: perDayValue("floors", (*garminconnect.Client).Floors, floorsAscended)},

	"endurance_score":   {fetch: fetchEnduranceScore},
	"hill_score":        {fetch: fetchHillScore},
	"running_tolerance": {fetch: fetchRunningTolerance},

	"sleep":             {fetchFrame: fetchSleep},
	"body_composition":  {fetchFrame: fetchBodyComposition},
	"blood_pressure":    {fetchFrame: fetchBloodPressure},
	"race_predictions":  {fetchFrame: fetchRacePredictions},
	"lactate_threshold": {fetchFrame: fetchLactateThreshold},
}

func day(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func gmtTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05.0", "2006-01-02T15:04:05", "2006-01-02 15:04:05.0", "2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// partialNotice reports requests the fan-out could not fetch, so sparse
// panels are distinguishable from sparse data.
func partialNotice(name string, failed, total int, firstErr error) []data.Notice {
	if failed == 0 {
		return nil
	}
	return []data.Notice{{
		Severity: data.NoticeSeverityWarning,
		Text:     fmt.Sprintf("%s: %d of %d requests failed (first error: %v)", name, failed, total, firstErr),
	}}
}

// perDay fetches a day-keyed Garmin resource for every day in the range with
// bounded concurrency. Days that fail are returned as nil; the failure count
// and first error are reported alongside so callers can distinguish "no data"
// from "all failed". Ranges beyond maxMetricDays are clamped to the most
// recent maxMetricDays days with a notice, matching how range endpoints
// degrade instead of erroring the whole panel.
func perDay[V any](ctx context.Context, client *garminconnect.Client, from, to time.Time, name string,
	get func(*garminconnect.Client, context.Context, time.Time) (*V, error),
) (days []time.Time, results []*V, failed int, notices []data.Notice, firstErr error) {
	for d := from.Truncate(24 * time.Hour); !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	if len(days) > maxMetricDays {
		requested := len(days)
		days = days[requested-maxMetricDays:]
		notices = append(notices, data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     fmt.Sprintf("%s needs one request per day; showing the most recent %d of the requested %d days", name, maxMetricDays, requested),
		})
	}

	results = make([]*V, len(days))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, d := range days {
		wg.Add(1)
		go func(i int, d time.Time) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dayCtx, span := startSpan(ctx, "garmin."+name+".day",
				attribute.String("metric", name), attribute.String("date", d.Format("2006-01-02")))
			v, err := get(client, dayCtx, d)
			endSpan(span, err)
			if err != nil {
				mu.Lock()
				failed++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			results[i] = v
		}(i, d)
	}
	wg.Wait()
	return days, results, failed, notices, firstErr
}

func perDayValue[V any](name string, get func(*garminconnect.Client, context.Context, time.Time) (*V, error),
	value func(*V) (float64, bool),
) metricFetch {
	return func(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
		days, results, failed, notices, firstErr := perDay(ctx, client, from, to, name, get)
		var points []metricPoint
		for i, r := range results {
			if r == nil {
				continue
			}
			if v, ok := value(r); ok {
				points = append(points, metricPoint{days[i], v})
			}
		}
		if len(points) == 0 && firstErr != nil {
			return nil, nil, firstErr
		}
		return points, append(notices, partialNotice(name, failed, len(days), firstErr)...), nil
	}
}

func firstTrainingReadiness(c *garminconnect.Client, ctx context.Context, d time.Time) (*garminconnect.TrainingReadiness, error) {
	entries, err := c.TrainingReadiness(ctx, d)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return &entries[0], nil
}

func floorsAscended(f *garminconnect.FloorsData) (float64, bool) {
	// FloorValuesArray rows: ["startGMT", "endGMT", ascended, descended]
	var rows [][]any
	if err := json.Unmarshal(f.FloorValuesArray, &rows); err != nil {
		return 0, false
	}
	total := 0.0
	for _, row := range rows {
		if len(row) >= 3 {
			if v, ok := row[2].(float64); ok {
				total += v
			}
		}
	}
	return total, total > 0
}

// Garmin range endpoints reject long spans with a 400; limits verified
// against the live API. Daily-steps and body-battery cap at about four
// weeks, the score endpoints (endurance, hill, running tolerance) at about
// a year.
const (
	chunkDaysWellness = 28
	chunkDaysScores   = 365
)

// chunked calls fetch for consecutive sub-ranges of at most days days and
// concatenates the results. Failed chunks are counted rather than aborting,
// so long ranges degrade to partial data instead of an all-or-nothing error;
// callers report the failures via partialNotice, mirroring perDay.
func chunked[T any](from, to time.Time, days int, fetch func(start, end time.Time) ([]T, error)) (all []T, failed, total int, firstErr error) {
	for start := from; !start.After(to); start = start.AddDate(0, 0, days) {
		end := start.AddDate(0, 0, days-1)
		if end.After(to) {
			end = to
		}
		total++
		batch, err := fetch(start, end)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, batch...)
	}
	return all, failed, total, firstErr
}

func fetchSteps(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	stats, failed, total, firstErr := chunked(from, to, chunkDaysWellness, func(start, end time.Time) ([]garminconnect.DailyStepStat, error) {
		return client.DailySteps(ctx, start, end)
	})
	var points []metricPoint
	for _, s := range stats {
		if t, ok := day(s.CalendarDate); ok {
			points = append(points, metricPoint{t, float64(s.TotalSteps)})
		}
	}
	if len(points) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return points, partialNotice("steps", failed, total, firstErr), nil
}

func fetchRestingHeartRate(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	resp, err := client.RestingHeartRate(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	var points []metricPoint
	for _, e := range resp.AllMetrics.MetricsMap.WellnessRestingHeartRate {
		if t, ok := day(e.CalendarDate); ok && e.Value > 0 {
			points = append(points, metricPoint{t, e.Value})
		}
	}
	return points, nil, nil
}

// fetchWeight reads the weight/dateRange endpoint; the weigh-in range
// endpoint returns a different shape than the client models and always
// parses empty.
func fetchWeight(ctx context.Context, d *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	resp, err := client.BodyComposition(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	var points []metricPoint
	for _, w := range resp.DateWeightList {
		if t, ok := day(w.CalendarDate); ok && w.Weight > 0 {
			points = append(points, metricPoint{t, d.massFromKg(w.Weight / 1000)}) // grams → kg → system
		}
	}
	return points, nil, nil
}

func fetchVO2Max(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	entries, err := client.MaxMetrics(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	var points []metricPoint
	for _, e := range entries {
		if e.Generic == nil || e.Generic.VO2MaxValue <= 0 {
			continue
		}
		if t, ok := day(e.Generic.CalendarDate); ok {
			points = append(points, metricPoint{t, e.Generic.VO2MaxValue})
		}
	}
	return points, nil, nil
}

// fetchFTP returns the latest cycling FTP as a single point; Garmin only
// exposes the current estimate, not its history. Accounts without a biometric
// FTP estimate often still carry a manually configured FTP in their power
// zone settings, so that is used as a fallback.
func fetchFTP(ctx context.Context, _ *Datasource, client *garminconnect.Client, _, to time.Time) ([]metricPoint, []data.Notice, error) {
	ftp, err := client.CyclingFTP(ctx)
	if err != nil {
		return nil, nil, err
	}
	if ftp.FunctionalThresholdPower != nil {
		t := to
		if ftp.CalendarDate != nil {
			if d, ok := day(*ftp.CalendarDate); ok {
				t = d
			}
		}
		return []metricPoint{{t, *ftp.FunctionalThresholdPower}}, nil, nil
	}

	zones, err := client.PowerZones(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, z := range zones {
		if z.Sport == "CYCLING" && z.FunctionalThresholdPower > 0 {
			return []metricPoint{{to, z.FunctionalThresholdPower}},
				[]data.Notice{{Severity: data.NoticeSeverityInfo, Text: "FTP taken from power zone settings; no biometric estimate available"}},
				nil
		}
	}
	return nil, nil, nil
}

func fetchBodyBattery(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	entries, failed, total, firstErr := chunked(from, to, chunkDaysWellness, func(start, end time.Time) ([]garminconnect.BodyBatteryEntry, error) {
		return client.BodyBattery(ctx, start, end)
	})
	var points []metricPoint
	for _, e := range entries {
		var rows [][]*float64
		if err := json.Unmarshal(e.BodyBatteryValues, &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if len(row) >= 2 && row[0] != nil && row[1] != nil {
				points = append(points, metricPoint{time.UnixMilli(int64(*row[0])), *row[1]})
			}
		}
	}
	if len(points) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return points, partialNotice("body_battery", failed, total, firstErr), nil
}

func fetchEnduranceScore(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	entries, failed, total, firstErr := chunked(from, to, chunkDaysScores, func(start, end time.Time) ([]garminconnect.EnduranceScoreEntry, error) {
		return client.EnduranceScore(ctx, start, end)
	})
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.Score > 0 {
			points = append(points, metricPoint{t, e.Score})
		}
	}
	if len(points) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return points, partialNotice("endurance_score", failed, total, firstErr), nil
}

func fetchHillScore(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	entries, failed, total, firstErr := chunked(from, to, chunkDaysScores, func(start, end time.Time) ([]garminconnect.HillScoreEntry, error) {
		return client.HillScore(ctx, start, end)
	})
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.HillScore > 0 {
			points = append(points, metricPoint{t, e.HillScore})
		}
	}
	if len(points) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return points, partialNotice("hill_score", failed, total, firstErr), nil
}

func fetchRunningTolerance(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) ([]metricPoint, []data.Notice, error) {
	entries, failed, total, firstErr := chunked(from, to, chunkDaysScores, func(start, end time.Time) ([]garminconnect.RunningToleranceEntry, error) {
		return client.RunningTolerance(ctx, start, end)
	})
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.Score > 0 {
			points = append(points, metricPoint{t, e.Score})
		}
	}
	if len(points) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}
	return points, partialNotice("running_tolerance", failed, total, firstErr), nil
}

func fetchSleep(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) (*data.Frame, []data.Notice, error) {
	days, results, failed, notices, firstErr := perDay(ctx, client, from, to, "sleep", (*garminconnect.Client).SleepData)
	var times []time.Time
	var total, deep, light, rem, awake []float64
	for i, s := range results {
		if s == nil || s.DailySleepDTO.SleepTimeSeconds <= 0 {
			continue
		}
		d := s.DailySleepDTO
		times = append(times, days[i])
		total = append(total, float64(d.SleepTimeSeconds))
		deep = append(deep, float64(d.DeepSleepSeconds))
		light = append(light, float64(d.LightSleepSeconds))
		rem = append(rem, float64(d.REMSleepSeconds))
		awake = append(awake, float64(d.AwakeSeconds))
	}
	if len(times) == 0 && firstErr != nil {
		return nil, nil, firstErr
	}

	frame := data.NewFrame("sleep",
		data.NewField("time", nil, times),
		data.NewField("total", nil, total),
		data.NewField("deep", nil, deep),
		data.NewField("light", nil, light),
		data.NewField("rem", nil, rem),
		data.NewField("awake", nil, awake),
	)
	for _, f := range frame.Fields[1:] {
		f.Config = &data.FieldConfig{Unit: "s"}
	}
	return frame, append(notices, partialNotice("sleep", failed, len(days), firstErr)...), nil
}

func fetchBodyComposition(ctx context.Context, d *Datasource, client *garminconnect.Client, from, to time.Time) (*data.Frame, []data.Notice, error) {
	resp, err := client.BodyComposition(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	var times []time.Time
	var weight, bmi, bodyFat, bodyWater, boneMass, muscleMass []*float64
	optional := func(v float64) *float64 {
		if v <= 0 {
			return nil
		}
		return &v
	}
	mass := func(grams float64) *float64 {
		if grams <= 0 {
			return nil
		}
		v := d.massFromKg(grams / 1000)
		return &v
	}
	// weight/dateRange returns newest-first; frames need ascending time.
	sort.Slice(resp.DateWeightList, func(i, j int) bool {
		return resp.DateWeightList[i].CalendarDate < resp.DateWeightList[j].CalendarDate
	})
	for _, w := range resp.DateWeightList {
		t, ok := day(w.CalendarDate)
		if !ok || w.Weight <= 0 {
			continue
		}
		times = append(times, t)
		weight = append(weight, mass(w.Weight))
		bmi = append(bmi, optional(w.Bmi))
		bodyFat = append(bodyFat, optional(w.BodyFat))
		bodyWater = append(bodyWater, optional(w.BodyWater))
		boneMass = append(boneMass, mass(w.BoneMass))
		muscleMass = append(muscleMass, mass(w.MuscleMass))
	}

	frame := data.NewFrame("body_composition",
		data.NewField("time", nil, times),
		data.NewField("weight", nil, weight),
		data.NewField("bmi", nil, bmi),
		data.NewField("body_fat", nil, bodyFat),
		data.NewField("body_water", nil, bodyWater),
		data.NewField("bone_mass", nil, boneMass),
		data.NewField("muscle_mass", nil, muscleMass),
	)
	setFieldUnits(frame, map[int]string{1: d.massUnitID(), 3: "percent", 4: "percent", 5: d.massUnitID(), 6: d.massUnitID()})
	return frame, nil, nil
}

func fetchBloodPressure(ctx context.Context, _ *Datasource, client *garminconnect.Client, from, to time.Time) (*data.Frame, []data.Notice, error) {
	resp, err := client.BloodPressure(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	var times []time.Time
	var systolic, diastolic, pulse []float64
	sort.Slice(resp.Measurements, func(i, j int) bool {
		return resp.Measurements[i].TimestampGMT < resp.Measurements[j].TimestampGMT
	})
	for _, m := range resp.Measurements {
		t, ok := gmtTime(m.TimestampGMT)
		if !ok {
			continue
		}
		times = append(times, t)
		systolic = append(systolic, float64(m.Systolic))
		diastolic = append(diastolic, float64(m.Diastolic))
		pulse = append(pulse, float64(m.Pulse))
	}

	frame := data.NewFrame("blood_pressure",
		data.NewField("time", nil, times),
		data.NewField("systolic", nil, systolic),
		data.NewField("diastolic", nil, diastolic),
		data.NewField("pulse", nil, pulse),
	)
	setFieldUnits(frame, map[int]string{1: "pressuremmhg", 2: "pressuremmhg"})
	return frame, nil, nil
}

// fetchRacePredictions returns Garmin's current predicted race times as a
// single point; history is not exposed.
func fetchRacePredictions(ctx context.Context, _ *Datasource, client *garminconnect.Client, _, to time.Time) (*data.Frame, []data.Notice, error) {
	p, err := client.RacePredictions(ctx)
	if err != nil {
		return nil, nil, err
	}
	t := to
	if d, ok := day(p.CalendarDate); ok {
		t = d
	}

	var times []time.Time
	var t5, t10, half, full []float64
	if p.Time5K > 0 || p.Time10K > 0 || p.TimeHalfMarathon > 0 || p.TimeMarathon > 0 {
		times = append(times, t)
		t5 = append(t5, float64(p.Time5K))
		t10 = append(t10, float64(p.Time10K))
		half = append(half, float64(p.TimeHalfMarathon))
		full = append(full, float64(p.TimeMarathon))
	}

	frame := data.NewFrame("race_predictions",
		data.NewField("time", nil, times),
		data.NewField("5k", nil, t5),
		data.NewField("10k", nil, t10),
		data.NewField("half_marathon", nil, half),
		data.NewField("marathon", nil, full),
	)
	for _, f := range frame.Fields[1:] {
		f.Config = &data.FieldConfig{Unit: "dthms"}
	}
	return frame, nil, nil
}

// fetchLactateThreshold returns the latest lactate threshold measurements;
// Garmin exposes one entry per sport, not a history.
func fetchLactateThreshold(ctx context.Context, d *Datasource, client *garminconnect.Client, _, to time.Time) (*data.Frame, []data.Notice, error) {
	entries, err := client.LactateThreshold(ctx)
	if err != nil {
		return nil, nil, err
	}
	var times []time.Time
	var speed, hrRunning, hrCycling []*float64
	intValue := func(v *int) *float64 {
		if v == nil {
			return nil
		}
		f := float64(*v)
		return &f
	}
	// API order is not guaranteed; lastNotNull reductions need ascending time.
	sort.Slice(entries, func(i, j int) bool { return entries[i].CalendarDate < entries[j].CalendarDate })
	for _, e := range entries {
		// calendarDate arrives as a plain date or a datetime, depending on
		// the measurement source.
		t, ok := day(e.CalendarDate)
		if !ok {
			t, ok = gmtTime(e.CalendarDate)
		}
		if !ok {
			t = to
		}
		times = append(times, t)
		if e.Speed != nil {
			// Garmin reports threshold speed in tens of m/s (0.35 → 3.5 m/s).
			converted := d.speedFromMS(*e.Speed * 10)
			speed = append(speed, &converted)
		} else {
			speed = append(speed, nil)
		}
		hrRunning = append(hrRunning, intValue(e.HearRate))
		hrCycling = append(hrCycling, intValue(e.HeartRateCycling))
	}

	frame := data.NewFrame("lactate_threshold",
		data.NewField("time", nil, times),
		data.NewField("speed", nil, speed),
		data.NewField("hr_running", nil, hrRunning),
		data.NewField("hr_cycling", nil, hrCycling),
	)
	setFieldUnits(frame, map[int]string{1: d.speedUnitID()})
	return frame, nil, nil
}
