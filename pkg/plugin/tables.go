package plugin

import (
	"context"
	"fmt"
	"time"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func tableResponse(frame *data.Frame) backend.DataResponse {
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
	var response backend.DataResponse
	response.Frames = append(response.Frames, frame)
	return response
}

// queryGear lists registered gear with lifetime usage. GearStats is one extra
// call per item; athletes have a handful of gear at most.
func (d *Datasource) queryGear(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	// The socialProfile endpoint returns no userProfilePK; the daily summary
	// carries the profile id the gear service expects.
	summary, err := client.UserSummary(ctx, time.Now())
	if err != nil {
		return errDownstream("fetch profile id: %v", err)
	}
	gear, err := client.Gear(ctx, summary.UserProfileID)
	if err != nil {
		return errDownstream("list gear: %v", err)
	}

	n := len(gear)
	names := make([]string, n)
	types := make([]string, n)
	statuses := make([]string, n)
	distances := make([]*float64, n)
	activities := make([]*int64, n)
	since := make([]*time.Time, n)
	for i, g := range gear {
		name := g.DisplayName
		if name == "" {
			name = g.CustomMakeModel
		}
		names[i] = name
		types[i] = g.GearTypeName
		statuses[i] = g.GearStatusName
		if t, ok := day(g.DateBegin); ok {
			since[i] = &t
		}
		if stats, err := client.GearStats(ctx, g.UUID); err == nil {
			dist := stats.TotalDistance
			count := int64(stats.TotalActivities)
			distances[i] = &dist
			activities[i] = &count
		}
	}

	frame := data.NewFrame("gear",
		data.NewField("name", nil, names),
		data.NewField("type", nil, types),
		data.NewField("status", nil, statuses),
		data.NewField("distance", nil, distances),
		data.NewField("activities", nil, activities),
		data.NewField("since", nil, since),
	)
	frame.Fields[3].Config = &data.FieldConfig{Unit: "lengthm"}
	return tableResponse(frame)
}

func (d *Datasource) queryDevices(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	devices, err := client.Devices(ctx)
	if err != nil {
		return errDownstream("list devices: %v", err)
	}

	n := len(devices)
	names := make([]string, n)
	products := make([]string, n)
	statuses := make([]string, n)
	registered := make([]string, n)
	for i, dev := range devices {
		names[i] = dev.DisplayName
		products[i] = dev.ProductDisplayName
		statuses[i] = dev.DeviceStatus
		registered[i] = dev.RegistrationDate.LocalRegistrationAppDate
	}

	frame := data.NewFrame("devices",
		data.NewField("name", nil, names),
		data.NewField("product", nil, products),
		data.NewField("status", nil, statuses),
		data.NewField("registered", nil, registered),
	)
	return tableResponse(frame)
}

// prTypeLabels maps Garmin personal record type ids to readable labels; the
// API leaves prTypeLabelKey empty.
var prTypeLabels = map[int64]string{
	1:  "Fastest 1K (run)",
	2:  "Fastest 1 mile (run)",
	3:  "Fastest 5K (run)",
	4:  "Fastest 10K (run)",
	7:  "Longest run",
	8:  "Longest ride",
	9:  "Total ascent (ride)",
	10: "Max avg power 20 min",
	12: "Most steps in a day",
	13: "Most steps in a week",
	14: "Most steps in a month",
	15: "Longest goal streak",
}

func (d *Datasource) queryPersonalRecords(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	records, err := client.PersonalRecords(ctx)
	if err != nil {
		return errDownstream("list personal records: %v", err)
	}

	n := len(records)
	labels := make([]string, n)
	typeIDs := make([]int64, n)
	values := make([]float64, n)
	times := make([]*time.Time, n)
	activityIDs := make([]int64, n)
	for i, r := range records {
		label := r.PrTypeLabelKey
		if label == "" {
			label = prTypeLabels[r.TypeID]
		}
		if label == "" {
			label = fmt.Sprintf("type_%d", r.TypeID)
		}
		labels[i] = label
		typeIDs[i] = r.TypeID
		values[i] = r.Value
		if t, ok := gmtTime(r.StartTimeGMT); ok {
			times[i] = &t
		}
		activityIDs[i] = r.ActivityID
	}

	frame := data.NewFrame("personal_records",
		data.NewField("record", nil, labels),
		data.NewField("type_id", nil, typeIDs),
		data.NewField("value", nil, values),
		data.NewField("time", nil, times),
		data.NewField("activity_id", nil, activityIDs),
	)
	return tableResponse(frame)
}

func (d *Datasource) querySplits(ctx context.Context, client *garminconnect.Client, id int64) backend.DataResponse {
	resp, err := client.ActivitySplits(ctx, id)
	if err != nil {
		return errDownstream("fetch splits for activity %d: %v", id, err)
	}

	n := len(resp.SplitSummaries)
	splits := make([]int64, n)
	times := make([]*time.Time, n)
	distances := make([]float64, n)
	durations := make([]float64, n)
	elevationGains := make([]float64, n)
	averageSpeeds := make([]float64, n)
	averageHRs := make([]float64, n)
	maxHRs := make([]float64, n)
	averagePowers := make([]float64, n)
	for i, s := range resp.SplitSummaries {
		splits[i] = int64(i + 1)
		if t, ok := gmtTime(s.StartTimeGMT); ok {
			times[i] = &t
		}
		distances[i] = s.Distance
		durations[i] = s.Duration
		elevationGains[i] = s.ElevationGain
		averageSpeeds[i] = s.AverageSpeed
		averageHRs[i] = s.AverageHR
		maxHRs[i] = s.MaxHR
		averagePowers[i] = s.AveragePower
	}

	frame := data.NewFrame("splits",
		data.NewField("split", nil, splits),
		data.NewField("time", nil, times),
		data.NewField("distance", nil, distances),
		data.NewField("duration", nil, durations),
		data.NewField("elevation_gain", nil, elevationGains),
		data.NewField("average_speed", nil, averageSpeeds),
		data.NewField("average_hr", nil, averageHRs),
		data.NewField("max_hr", nil, maxHRs),
		data.NewField("average_power", nil, averagePowers),
	)
	units := map[int]string{2: "lengthm", 3: "s", 4: "lengthm", 5: "velocityms", 8: "watt"}
	for i, u := range units {
		frame.Fields[i].Config = &data.FieldConfig{Unit: u}
	}
	return tableResponse(frame)
}

func (d *Datasource) queryHRZones(ctx context.Context, client *garminconnect.Client, id int64) backend.DataResponse {
	zones, err := client.ActivityHRZones(ctx, id)
	if err != nil {
		return errDownstream("fetch HR zones for activity %d: %v", id, err)
	}

	n := len(zones)
	numbers := make([]int64, n)
	seconds := make([]float64, n)
	lows := make([]int64, n)
	highs := make([]int64, n)
	for i, z := range zones {
		numbers[i] = int64(z.ZoneNumber)
		seconds[i] = z.SecsInZone
		lows[i] = int64(z.ZoneLowBPM)
		highs[i] = int64(z.ZoneHighBPM)
	}

	frame := data.NewFrame("hr_zones",
		data.NewField("zone", nil, numbers),
		data.NewField("time_in_zone", nil, seconds),
		data.NewField("low", nil, lows),
		data.NewField("high", nil, highs),
	)
	frame.Fields[1].Config = &data.FieldConfig{Unit: "s"}
	return tableResponse(frame)
}
