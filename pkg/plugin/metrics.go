package plugin

import (
	"context"
	"encoding/json"
	"fmt"
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

// metricDef describes one selectable metric. Single-series metrics set fetch
// and optionally unit; metrics with several related series (sleep stages,
// body composition, ...) set fetchFrame instead and build their own frame.
type metricDef struct {
	unit       string
	fetch      func(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error)
	fetchFrame func(ctx context.Context, client *garminconnect.Client, from, to time.Time) (*data.Frame, error)
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
	"intensity_minutes": {unit: "m", fetch: perDayValue("intensity_minutes", (*garminconnect.Client).IntensityMinutes,
		func(m *garminconnect.IntensityMinutesData) (float64, bool) {
			total := m.ModerateIntensityMinutes + m.VigorousIntensityMinutes
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

// perDay fetches a day-keyed Garmin resource for every day in the range with
// bounded concurrency. Days that fail are returned as nil; the first error is
// reported alongside so callers can distinguish "no data" from "all failed".
func perDay[V any](ctx context.Context, client *garminconnect.Client, from, to time.Time, name string,
	get func(*garminconnect.Client, context.Context, time.Time) (*V, error),
) ([]time.Time, []*V, error) {
	var days []time.Time
	for d := from.Truncate(24 * time.Hour); !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	if len(days) > maxMetricDays {
		return nil, nil, fmt.Errorf("time range spans %d days; %s supports at most %d", len(days), name, maxMetricDays)
	}

	results := make([]*V, len(days))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i, d := range days {
		wg.Add(1)
		go func(i int, d time.Time) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dayCtx, span := startSpan(ctx, "garmin.day",
				attribute.String("metric", name), attribute.String("date", d.Format("2006-01-02")))
			v, err := get(client, dayCtx, d)
			endSpan(span, err)
			if err != nil {
				mu.Lock()
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
	return days, results, firstErr
}

func perDayValue[V any](name string, get func(*garminconnect.Client, context.Context, time.Time) (*V, error),
	value func(*V) (float64, bool),
) func(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	return func(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
		days, results, firstErr := perDay(ctx, client, from, to, name, get)
		if results == nil {
			return nil, firstErr
		}
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
			return nil, firstErr
		}
		return points, nil
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

// stepsChunkDays keeps requests under the daily-steps endpoint's range limit
// (Garmin rejects spans much beyond four weeks with a 400).
const stepsChunkDays = 28

func fetchSteps(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	var points []metricPoint
	for start := from; !start.After(to); start = start.AddDate(0, 0, stepsChunkDays) {
		end := start.AddDate(0, 0, stepsChunkDays-1)
		if end.After(to) {
			end = to
		}
		stats, err := client.DailySteps(ctx, start, end)
		if err != nil {
			return nil, err
		}
		for _, s := range stats {
			if t, ok := day(s.CalendarDate); ok {
				points = append(points, metricPoint{t, float64(s.TotalSteps)})
			}
		}
	}
	return points, nil
}

func fetchRestingHeartRate(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	resp, err := client.RestingHeartRate(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var points []metricPoint
	for _, e := range resp.AllMetrics.MetricsMap.WellnessRestingHeartRate {
		if t, ok := day(e.CalendarDate); ok && e.Value > 0 {
			points = append(points, metricPoint{t, e.Value})
		}
	}
	return points, nil
}

func fetchWeight(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	resp, err := client.WeighIns(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var points []metricPoint
	for _, w := range resp.DateWeightList {
		if t, ok := day(w.CalendarDate); ok && w.Weight > 0 {
			points = append(points, metricPoint{t, w.Weight / 1000}) // grams → kg
		}
	}
	return points, nil
}

func fetchVO2Max(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	entries, err := client.MaxMetrics(ctx, from, to)
	if err != nil {
		return nil, err
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
	return points, nil
}

// fetchFTP returns the latest cycling FTP as a single point; Garmin only
// exposes the current estimate, not its history.
func fetchFTP(ctx context.Context, client *garminconnect.Client, _, to time.Time) ([]metricPoint, error) {
	ftp, err := client.CyclingFTP(ctx)
	if err != nil {
		return nil, err
	}
	if ftp.FunctionalThresholdPower == nil {
		return nil, nil
	}
	t := to
	if ftp.CalendarDate != nil {
		if d, ok := day(*ftp.CalendarDate); ok {
			t = d
		}
	}
	return []metricPoint{{t, *ftp.FunctionalThresholdPower}}, nil
}

func fetchBodyBattery(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	entries, err := client.BodyBattery(ctx, from, to)
	if err != nil {
		return nil, err
	}
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
	return points, nil
}

func fetchEnduranceScore(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	entries, err := client.EnduranceScore(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.Score > 0 {
			points = append(points, metricPoint{t, e.Score})
		}
	}
	return points, nil
}

func fetchHillScore(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	entries, err := client.HillScore(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.HillScore > 0 {
			points = append(points, metricPoint{t, e.HillScore})
		}
	}
	return points, nil
}

func fetchRunningTolerance(ctx context.Context, client *garminconnect.Client, from, to time.Time) ([]metricPoint, error) {
	entries, err := client.RunningTolerance(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var points []metricPoint
	for _, e := range entries {
		if t, ok := day(e.CalendarDate); ok && e.Score > 0 {
			points = append(points, metricPoint{t, e.Score})
		}
	}
	return points, nil
}

func fetchSleep(ctx context.Context, client *garminconnect.Client, from, to time.Time) (*data.Frame, error) {
	days, results, firstErr := perDay(ctx, client, from, to, "sleep", (*garminconnect.Client).SleepData)
	if results == nil {
		return nil, firstErr
	}
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
		return nil, firstErr
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
	return frame, nil
}

func fetchBodyComposition(ctx context.Context, client *garminconnect.Client, from, to time.Time) (*data.Frame, error) {
	resp, err := client.WeighIns(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var times []time.Time
	var weight, bmi, bodyFat, bodyWater, boneMass, muscleMass []*float64
	optional := func(v float64) *float64 {
		if v <= 0 {
			return nil
		}
		return &v
	}
	for _, w := range resp.DateWeightList {
		t, ok := day(w.CalendarDate)
		if !ok || w.Weight <= 0 {
			continue
		}
		times = append(times, t)
		kg := w.Weight / 1000
		weight = append(weight, &kg)
		bmi = append(bmi, optional(w.Bmi))
		bodyFat = append(bodyFat, optional(w.BodyFat))
		bodyWater = append(bodyWater, optional(w.BodyWater))
		boneMass = append(boneMass, optional(w.BoneMass/1000))
		muscleMass = append(muscleMass, optional(w.MuscleMass/1000))
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
	units := []string{"masskg", "", "percent", "percent", "masskg", "masskg"}
	for i, u := range units {
		if u != "" {
			frame.Fields[i+1].Config = &data.FieldConfig{Unit: u}
		}
	}
	return frame, nil
}

func fetchBloodPressure(ctx context.Context, client *garminconnect.Client, from, to time.Time) (*data.Frame, error) {
	resp, err := client.BloodPressure(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var times []time.Time
	var systolic, diastolic, pulse []float64
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
	frame.Fields[1].Config = &data.FieldConfig{Unit: "pressuremmhg"}
	frame.Fields[2].Config = &data.FieldConfig{Unit: "pressuremmhg"}
	return frame, nil
}

// fetchRacePredictions returns Garmin's current predicted race times as a
// single point; history is not exposed.
func fetchRacePredictions(ctx context.Context, client *garminconnect.Client, _, to time.Time) (*data.Frame, error) {
	p, err := client.RacePredictions(ctx)
	if err != nil {
		return nil, err
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
		f.Config = &data.FieldConfig{Unit: "dtdurations"}
	}
	return frame, nil
}

// fetchLactateThreshold returns the latest lactate threshold measurements;
// Garmin exposes one entry per sport, not a history.
func fetchLactateThreshold(ctx context.Context, client *garminconnect.Client, _, to time.Time) (*data.Frame, error) {
	entries, err := client.LactateThreshold(ctx)
	if err != nil {
		return nil, err
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
	for _, e := range entries {
		t, ok := day(e.CalendarDate)
		if !ok {
			t = to
		}
		times = append(times, t)
		speed = append(speed, e.Speed)
		hrRunning = append(hrRunning, intValue(e.HearRate))
		hrCycling = append(hrCycling, intValue(e.HeartRateCycling))
	}

	frame := data.NewFrame("lactate_threshold",
		data.NewField("time", nil, times),
		data.NewField("speed", nil, speed),
		data.NewField("hr_running", nil, hrRunning),
		data.NewField("hr_cycling", nil, hrCycling),
	)
	frame.Fields[1].Config = &data.FieldConfig{Unit: "velocityms"}
	return frame, nil
}
