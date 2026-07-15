package plugin

import (
	"encoding/json"
	"fmt"
	"time"
)

// detailSeries extracts one per-sample metric from Garmin's activity details
// response, keyed by descriptor name (e.g. "directPower"). Activities without
// samples for the metric return empty slices.
func detailSeries(details map[string]json.RawMessage, metricKey string) ([]time.Time, []*float64, error) {
	rawDescriptors, ok := details["metricDescriptors"]
	if !ok {
		return nil, nil, nil
	}
	var descriptors []struct {
		MetricsIndex int    `json:"metricsIndex"`
		Key          string `json:"key"`
	}
	if err := json.Unmarshal(rawDescriptors, &descriptors); err != nil {
		return nil, nil, fmt.Errorf("parse metric descriptors: %w", err)
	}

	timeIdx, valueIdx := -1, -1
	for _, d := range descriptors {
		switch d.Key {
		case "directTimestamp":
			timeIdx = d.MetricsIndex
		case metricKey:
			valueIdx = d.MetricsIndex
		}
	}
	rawMetrics, ok := details["activityDetailMetrics"]
	if timeIdx < 0 || valueIdx < 0 || !ok {
		return nil, nil, nil
	}

	var rows []struct {
		Metrics []*float64 `json:"metrics"`
	}
	if err := json.Unmarshal(rawMetrics, &rows); err != nil {
		return nil, nil, fmt.Errorf("parse detail metrics: %w", err)
	}
	times := make([]time.Time, 0, len(rows))
	values := make([]*float64, 0, len(rows))
	for _, r := range rows {
		if timeIdx >= len(r.Metrics) || valueIdx >= len(r.Metrics) || r.Metrics[timeIdx] == nil {
			continue
		}
		times = append(times, time.UnixMilli(int64(*r.Metrics[timeIdx])).UTC())
		values = append(values, r.Metrics[valueIdx])
	}
	return times, values, nil
}
