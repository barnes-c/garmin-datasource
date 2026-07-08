package plugin

import (
	"context"
	"fmt"
	"math"
	"strconv"
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
		if t, ok := gmtTime(g.DateBegin); ok {
			since[i] = &t
		}
		if stats, err := client.GearStats(ctx, g.UUID); err == nil {
			dist := d.distanceFromMeters(stats.TotalDistance)
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
		data.NewField("used_since", nil, since),
	)
	frame.Fields[3].Config = &data.FieldConfig{Unit: d.distanceUnitID()}
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
	serials := make([]string, n)
	firmwares := make([]string, n)
	registered := make([]*time.Time, n)
	for i, dev := range devices {
		names[i] = dev.DisplayName
		products[i] = dev.ProductDisplayName
		statuses[i] = dev.DeviceStatus
		serials[i] = dev.SerialNumber
		firmwares[i] = dev.CurrentFirmwareVersion
		if dev.RegisteredDate > 0 {
			t := time.UnixMilli(dev.RegisteredDate)
			registered[i] = &t
		}
	}

	frame := data.NewFrame("devices",
		data.NewField("name", nil, names),
		data.NewField("product", nil, products),
		data.NewField("status", nil, statuses),
		data.NewField("serial", nil, serials),
		data.NewField("firmware", nil, firmwares),
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
	11: "Fastest 40K (ride)",
	12: "Most steps in a day",
	13: "Most steps in a week",
	14: "Most steps in a month",
	15: "Longest goal streak",
	16: "Current goal streak",
	17: "Longest swim",
	18: "Fastest 100 m (swim)",
	20: "Fastest 400 m (swim)",
}

// formatPRValue renders a personal record value in the unit implied by its
// type; the raw column mixes seconds, meters, steps and days, which Grafana
// cannot format per row.
func (d *Datasource) formatPRValue(typeID int64, v float64) string {
	switch typeID {
	case 1, 2, 3, 4, 11, 18, 20: // fastest run/ride/swim times, seconds
		return formatDuration(v)
	case 7, 8: // longest run/ride, meters
		if d.imperial() {
			return fmt.Sprintf("%.2f mi", v/metersPerMile)
		}
		return fmt.Sprintf("%.2f km", v/1000)
	case 9: // total ascent, meters
		if d.imperial() {
			return fmt.Sprintf("%.0f ft", v*feetPerMeter)
		}
		return fmt.Sprintf("%.0f m", v)
	case 17: // longest swim; pools are measured in meters either way
		return fmt.Sprintf("%.0f m", v)
	case 10: // max avg power 20 min
		return fmt.Sprintf("%.0f W", v)
	case 12, 13, 14: // most steps day/week/month
		return fmt.Sprintf("%.0f steps", v)
	case 15, 16: // longest/current goal streak
		return fmt.Sprintf("%.0f days", v)
	default:
		return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	}
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func (d *Datasource) queryPersonalRecords(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	records, err := client.PersonalRecords(ctx)
	if err != nil {
		return errDownstream("list personal records: %v", err)
	}

	n := len(records)
	labels := make([]string, n)
	typeIDs := make([]int64, n)
	values := make([]string, n)
	rawValues := make([]float64, n)
	times := make([]*time.Time, n)
	sports := make([]string, n)
	activityNames := make([]string, n)
	activityIDs := make([]int64, n)
	for i, r := range records {
		sports[i] = r.ActivityType
		label := r.PrTypeLabelKey
		if label == "" {
			label = prTypeLabels[r.TypeID]
		}
		if label == "" {
			label = fmt.Sprintf("type_%d", r.TypeID)
		}
		labels[i] = label
		typeIDs[i] = r.TypeID
		values[i] = d.formatPRValue(r.TypeID, r.Value)
		rawValues[i] = r.Value
		if r.PrStartTimeGmt > 0 {
			t := time.UnixMilli(r.PrStartTimeGmt)
			times[i] = &t
		}
		activityNames[i] = r.ActivityName
		activityIDs[i] = r.ActivityID
	}

	frame := data.NewFrame("personal_records",
		data.NewField("record", nil, labels),
		data.NewField("value", nil, values),
		data.NewField("time", nil, times),
		data.NewField("sport", nil, sports),
		data.NewField("activity", nil, activityNames),
		data.NewField("activity_id", nil, activityIDs),
		data.NewField("type_id", nil, typeIDs),
		data.NewField("raw_value", nil, rawValues),
	)
	return tableResponse(frame)
}

func (d *Datasource) queryHRZoneConfig(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	configs, err := client.HeartRateZones(ctx)
	if err != nil {
		return errDownstream("fetch HR zone config: %v", err)
	}

	n := len(configs)
	sports := make([]string, n)
	methods := make([]string, n)
	z1 := make([]int64, n)
	z2 := make([]int64, n)
	z3 := make([]int64, n)
	z4 := make([]int64, n)
	z5 := make([]int64, n)
	maxHRs := make([]int64, n)
	lthrs := make([]int64, n)
	restingHRs := make([]int64, n)
	for i, c := range configs {
		sports[i] = c.Sport
		methods[i] = c.TrainingMethod
		z1[i] = int64(c.Zone1Floor)
		z2[i] = int64(c.Zone2Floor)
		z3[i] = int64(c.Zone3Floor)
		z4[i] = int64(c.Zone4Floor)
		z5[i] = int64(c.Zone5Floor)
		maxHRs[i] = int64(c.MaxHeartRateUsed)
		lthrs[i] = int64(c.LactateThresholdHeartRateUsed)
		restingHRs[i] = int64(c.RestingHeartRateUsed)
	}

	frame := data.NewFrame("hr_zone_config",
		data.NewField("sport", nil, sports),
		data.NewField("method", nil, methods),
		data.NewField("zone1_floor", nil, z1),
		data.NewField("zone2_floor", nil, z2),
		data.NewField("zone3_floor", nil, z3),
		data.NewField("zone4_floor", nil, z4),
		data.NewField("zone5_floor", nil, z5),
		data.NewField("max_hr", nil, maxHRs),
		data.NewField("lthr", nil, lthrs),
		data.NewField("resting_hr", nil, restingHRs),
	)
	return tableResponse(frame)
}

func (d *Datasource) queryPowerZoneConfig(ctx context.Context, client *garminconnect.Client) backend.DataResponse {
	configs, err := client.PowerZones(ctx)
	if err != nil {
		return errDownstream("fetch power zone config: %v", err)
	}

	n := len(configs)
	sports := make([]string, n)
	ftps := make([]float64, n)
	floors := make([][]float64, 7)
	for z := range floors {
		floors[z] = make([]float64, n)
	}
	for i, c := range configs {
		sports[i] = c.Sport
		ftps[i] = c.FunctionalThresholdPower
		for z, v := range []float64{c.Zone1Floor, c.Zone2Floor, c.Zone3Floor, c.Zone4Floor, c.Zone5Floor, c.Zone6Floor, c.Zone7Floor} {
			floors[z][i] = v
		}
	}

	frame := data.NewFrame("power_zone_config",
		data.NewField("sport", nil, sports),
		data.NewField("ftp", nil, ftps),
	)
	for z, values := range floors {
		frame.Fields = append(frame.Fields, data.NewField(fmt.Sprintf("zone%d_floor", z+1), nil, values))
	}
	setFieldUnits(frame, map[int]string{1: "watt", 2: "watt", 3: "watt", 4: "watt", 5: "watt", 6: "watt", 7: "watt", 8: "watt"})
	return tableResponse(frame)
}

func (d *Datasource) querySplits(ctx context.Context, client *garminconnect.Client, id int64) backend.DataResponse {
	resp, err := client.ActivitySplits(ctx, id)
	if err != nil {
		return errDownstream("fetch splits for activity %d: %v", id, err)
	}
	// Regular laps live in LapDTOs; SplitSummaries only covers structured
	// workouts.
	splitList := resp.LapDTOs
	if len(splitList) == 0 {
		splitList = resp.SplitSummaries
	}

	n := len(splitList)
	splits := make([]int64, n)
	times := make([]*time.Time, n)
	distances := make([]float64, n)
	durations := make([]float64, n)
	elevationGains := make([]float64, n)
	averageSpeeds := make([]float64, n)
	averageHRs := make([]float64, n)
	maxHRs := make([]float64, n)
	averagePowers := make([]float64, n)
	for i, s := range splitList {
		splits[i] = int64(i + 1)
		if t, ok := gmtTime(s.StartTimeGMT); ok {
			times[i] = &t
		}
		distances[i] = d.distanceFromMeters(s.Distance)
		durations[i] = s.Duration
		elevationGains[i] = d.elevationFromMeters(s.ElevationGain)
		averageSpeeds[i] = d.speedFromMS(s.AverageSpeed)
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
	setFieldUnits(frame, map[int]string{2: d.distanceUnitID(), 3: "s", 4: d.elevationUnitID(), 5: d.speedUnitID(), 8: "watt"})
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
