package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"

	"github.com/barnesc/garminconnect/pkg/models"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

const (
	queryTypeActivities      = "activities"
	queryTypeTrack           = "track"
	queryTypeMetric          = "metric"
	queryTypeGear            = "gear"
	queryTypeDevices         = "devices"
	queryTypePersonalRecords = "personal_records"
	queryTypeSplits          = "splits"
	queryTypeHRZones         = "hr_zones"

	maxCachedTracks = 100

	mfaCodeWait = 5 * time.Minute

	// Two cache regimes: data touching the last 24h still changes (today's
	// metrics tick up, new activities sync in), so it expires quickly to
	// absorb dashboard refreshes and per-minute alert evaluations. Ranges
	// that ended over a day ago are settled — Garmin has finalized sleep/HRV
	// and backfilled uploads are rare — and can live a day. The buffer also
	// covers timezone skew between server UTC and Garmin's local calendar days.
	frameCacheTTLCurrent    = 5 * time.Minute
	frameCacheTTLHistorical = 24 * time.Hour

	// Bounds memory only; eviction is clear-all, so this just needs to be
	// comfortably above the working set (keys grow ~1/day per distinct query).
	maxCachedFrames = 512
)

var errMFAPending = errors.New("Garmin sent an MFA code to your email; enter it in the datasource settings and click Verify")

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	config, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}
	return &Datasource{
		settings:   config,
		tracks:     map[int64][]trackPoint{},
		frameCache: map[string]cachedFrame{},
	}, nil
}

type Datasource struct {
	settings *models.PluginSettings

	mu     sync.Mutex
	client *garminconnect.Client
	login  *loginAttempt

	trackMu sync.Mutex
	tracks  map[int64][]trackPoint

	frameMu    sync.Mutex
	frameCache map[string]cachedFrame

	group singleflight.Group
}

// coalesce collapses concurrent identical fetches — e.g. several panels of a
// cold dashboard issuing the same query — into a single Garmin call whose
// result is shared by all callers.
func (d *Datasource) coalesce(key string, fetch func() backend.DataResponse) backend.DataResponse {
	v, _, _ := d.group.Do(key, func() (any, error) { return fetch(), nil })
	return v.(backend.DataResponse)
}

type cachedFrame struct {
	frame   *data.Frame
	expires time.Time
}

func (d *Datasource) cacheGet(key string) (*data.Frame, bool) {
	d.frameMu.Lock()
	defer d.frameMu.Unlock()
	if c, ok := d.frameCache[key]; ok && time.Now().Before(c.expires) {
		return c.frame, true
	}
	return nil, false
}

func (d *Datasource) cachePut(key string, frame *data.Frame, ttl time.Duration) {
	d.frameMu.Lock()
	defer d.frameMu.Unlock()
	if len(d.frameCache) >= maxCachedFrames {
		d.frameCache = map[string]cachedFrame{}
	}
	d.frameCache[key] = cachedFrame{frame: frame, expires: time.Now().Add(ttl)}
}

// rangeTTL picks the cache lifetime based on whether the queried range can
// still receive new data.
func rangeTTL(timeRange backend.TimeRange) time.Duration {
	if time.Since(timeRange.To) > 24*time.Hour {
		return frameCacheTTLHistorical
	}
	return frameCacheTTLCurrent
}

func frameResponse(frame *data.Frame) backend.DataResponse {
	var response backend.DataResponse
	response.Frames = append(response.Frames, frame)
	return response
}

// dayRange keys a cache entry by day-truncated time range; all cached Garmin
// endpoints take date parameters, so sub-day range differences cannot change
// the result.
func dayRange(timeRange backend.TimeRange) string {
	return timeRange.From.Truncate(24*time.Hour).Format("2006-01-02") + "|" +
		timeRange.To.Truncate(24*time.Hour).Format("2006-01-02")
}

// loginAttempt is one background Login call. When Garmin requests MFA and no
// code is configured, the attempt closes mfaNeeded and blocks until a code
// arrives on the code channel (delivered by the /mfa resource endpoint).
type loginAttempt struct {
	mfaOnce   sync.Once
	mfaNeeded chan struct{}
	code      chan string
	done      chan struct{}
	client    *garminconnect.Client
	err       error
}

func (d *Datasource) Dispose() {}

// startLoginLocked launches a background login. d.mu must be held. The login
// runs on a background context because completing it can take minutes when
// the user still has to type an MFA code.
func (d *Datasource) startLoginLocked() *loginAttempt {
	a := &loginAttempt{
		mfaNeeded: make(chan struct{}),
		code:      make(chan string, 1),
		done:      make(chan struct{}),
	}
	d.login = a

	email := d.settings.Email
	password := d.settings.Secrets.Password
	configuredCode := d.settings.Secrets.MFACode
	tokenFile := d.settings.TokenFile

	go func() {
		defer close(a.done)
		logger := log.DefaultLogger

		if tokenFile != "" {
			if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
				a.err = fmt.Errorf("create token file directory: %w", err)
				return
			}
		}

		prompt := func() (string, error) {
			if configuredCode != "" {
				return configuredCode, nil
			}
			logger.Info("Garmin requested an MFA code, waiting for the user to submit one")
			a.mfaOnce.Do(func() { close(a.mfaNeeded) })
			select {
			case code := <-a.code:
				logger.Info("MFA code received, resuming login")
				return code, nil
			case <-time.After(mfaCodeWait):
				return "", errors.New("timed out waiting for MFA code")
			}
		}

		logger.Info("Starting Garmin Connect login", "tokenFileConfigured", tokenFile != "")
		start := time.Now()
		loginCtx, span := startSpan(context.Background(), "garmin.login")
		client := garminconnect.NewClient(tokenFile, garminconnect.WithMFAPrompt(prompt))
		err := client.Login(loginCtx, email, password)
		endSpan(span, err)
		if err != nil {
			logger.Error("Garmin Connect login failed", "error", err)
			a.err = fmt.Errorf("garmin login: %w", err)
			return
		}
		logger.Info("Garmin Connect login succeeded", "displayName", client.DisplayName(), "duration", time.Since(start).String())
		a.client = client
	}()
	return a
}

// finishLogin publishes the attempt's outcome. Failed attempts are cleared so
// the next call retries with a fresh login.
func (d *Datasource) finishLogin(a *loginAttempt) (*garminconnect.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.login == a {
		d.login = nil
		if a.err == nil {
			d.client = a.client
		}
	}
	if a.err != nil {
		return nil, a.err
	}
	return a.client, nil
}

// garminClient returns a logged-in client. With a token file configured (same
// convention as garmin_exporter's --token-file), the login resumes or
// refreshes the persisted OAuth token and only falls back to a full SSO login
// (and MFA) when neither works.
func (d *Datasource) garminClient(ctx context.Context) (*garminconnect.Client, error) {
	d.mu.Lock()
	if d.client != nil {
		client := d.client
		d.mu.Unlock()
		return client, nil
	}
	if d.settings.Email == "" || d.settings.Secrets.Password == "" {
		d.mu.Unlock()
		return nil, errors.New("email and password must be configured")
	}
	a := d.login
	if a == nil {
		a = d.startLoginLocked()
	}
	d.mu.Unlock()

	// Prefer a finished login over a stale MFA signal: once the code has been
	// delivered, done closes shortly after mfaNeeded and both selects race.
	select {
	case <-a.done:
		return d.finishLogin(a)
	default:
	}
	select {
	case <-a.done:
		return d.finishLogin(a)
	case <-a.mfaNeeded:
		return nil, errMFAPending
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.settings.Email == "" || d.settings.Secrets.Password == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Email and password are required",
		}, nil
	}
	client, err := d.garminClient(ctx)
	if err != nil {
		message := err.Error()
		if errors.Is(err, errMFAPending) {
			message = "Garmin sent an MFA code to your email. Enter it in the MFA code field and click Verify."
		}
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: message,
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: fmt.Sprintf("Successfully authenticated with Garmin Connect as %s", client.DisplayName()),
	}, nil
}

// CallResource handles POST /mfa, feeding the emailed code into the login
// attempt that is blocked waiting for it.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Path != "mfa" || req.Method != http.MethodPost {
		return resourceJSON(sender, http.StatusNotFound, "not found")
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil || strings.TrimSpace(body.Code) == "" {
		return resourceJSON(sender, http.StatusBadRequest, "an MFA code is required")
	}

	d.mu.Lock()
	a := d.login
	d.mu.Unlock()

	waiting := a != nil
	if waiting {
		select {
		case <-a.mfaNeeded:
		default:
			waiting = false
		}
	}
	if !waiting {
		return resourceJSON(sender, http.StatusConflict, "no login is waiting for an MFA code; click Save & test first")
	}

	select {
	case a.code <- strings.TrimSpace(body.Code):
	default:
		return resourceJSON(sender, http.StatusConflict, "an MFA code was already submitted for this login")
	}

	select {
	case <-a.done:
	case <-ctx.Done():
		return resourceJSON(sender, http.StatusGatewayTimeout, "timed out waiting for the login to finish")
	}
	if _, err := d.finishLogin(a); err != nil {
		return resourceJSON(sender, http.StatusUnauthorized, err.Error())
	}
	return resourceJSON(sender, http.StatusOK, "MFA verified — logged in to Garmin Connect")
}

// errDownstream reports a failed Garmin API call, attributed to the upstream
// service rather than the plugin in Grafana's error metrics.
func errDownstream(format string, args ...any) backend.DataResponse {
	response := backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf(format, args...))
	response.ErrorSource = backend.ErrorSourceDownstream
	return response
}

func resourceJSON(sender backend.CallResourceResponseSender, status int, message string) error {
	body, _ := json.Marshal(map[string]string{"message": message})
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body,
	})
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

type queryModel struct {
	ActivityID   string `json:"activityId"`
	ActivityType string `json:"activityType"`
	Limit        int    `json:"limit"`
	Metric       string `json:"metric"`
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("json unmarshal: %v", err))
	}

	switch query.QueryType {
	case queryTypeTrack:
		return d.queryTrack(ctx, qm)
	case queryTypeMetric:
		return d.queryMetric(ctx, qm, query.TimeRange)
	case queryTypeActivities, "":
		return d.queryActivities(ctx, qm, query.TimeRange)
	case queryTypeGear, queryTypeDevices, queryTypePersonalRecords, queryTypeSplits, queryTypeHRZones:
		return d.queryTable(ctx, query.QueryType, qm)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("unknown query type %q", query.QueryType))
	}
}

func (d *Datasource) queryTable(ctx context.Context, queryType string, qm queryModel) backend.DataResponse {
	key := "table|" + queryType + "|" + qm.ActivityID
	if frame, ok := d.cacheGet(key); ok {
		return frameResponse(frame)
	}

	return d.coalesce(key, func() backend.DataResponse {
		return d.fetchTable(ctx, key, queryType, qm)
	})
}

func (d *Datasource) fetchTable(ctx context.Context, key, queryType string, qm queryModel) backend.DataResponse {
	client, err := d.garminClient(ctx)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, err.Error())
	}

	fetchCtx, span := startSpan(ctx, "garmin."+queryType)
	defer func() { span.End() }()

	var response backend.DataResponse
	switch queryType {
	case queryTypeGear:
		response = d.queryGear(fetchCtx, client)
	case queryTypeDevices:
		response = d.queryDevices(fetchCtx, client)
	case queryTypePersonalRecords:
		response = d.queryPersonalRecords(fetchCtx, client)
	case queryTypeSplits, queryTypeHRZones:
		if qm.ActivityID == "" {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("activity id is required for %s queries", queryType))
		}
		id, err := strconv.ParseInt(qm.ActivityID, 10, 64)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid activity id %q", qm.ActivityID))
		}
		if queryType == queryTypeSplits {
			response = d.querySplits(fetchCtx, client, id)
		} else {
			response = d.queryHRZones(fetchCtx, client, id)
		}
	}

	if response.Error == nil && len(response.Frames) == 1 {
		// splits and hr_zones are immutable per activity; gear, devices and
		// personal records change as new activities sync in.
		ttl := frameCacheTTLCurrent
		if queryType == queryTypeSplits || queryType == queryTypeHRZones {
			ttl = frameCacheTTLHistorical
		}
		d.cachePut(key, response.Frames[0], ttl)
	}
	return response
}

func (d *Datasource) queryActivities(ctx context.Context, qm queryModel, timeRange backend.TimeRange) backend.DataResponse {
	key := fmt.Sprintf("activities|%s|%d|%s", qm.ActivityType, qm.Limit, dayRange(timeRange))
	if frame, ok := d.cacheGet(key); ok {
		return frameResponse(frame)
	}

	return d.coalesce(key, func() backend.DataResponse {
		return d.fetchActivities(ctx, key, qm, timeRange)
	})
}

func (d *Datasource) fetchActivities(ctx context.Context, key string, qm queryModel, timeRange backend.TimeRange) backend.DataResponse {
	client, err := d.garminClient(ctx)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, err.Error())
	}

	spanCtx, span := startSpan(ctx, "garmin.activities", attribute.String("activity_type", qm.ActivityType))
	activities, err := client.ActivitiesByDate(spanCtx, timeRange.From, timeRange.To, qm.ActivityType)
	endSpan(span, err)
	if err != nil {
		return errDownstream("list activities: %v", err)
	}
	if qm.Limit > 0 && len(activities) > qm.Limit {
		activities = activities[:qm.Limit]
	}

	n := len(activities)
	ids := make([]int64, n)
	names := make([]string, n)
	types := make([]string, n)
	starts := make([]time.Time, n)
	distances := make([]float64, n)
	durations := make([]float64, n)
	movingDurations := make([]float64, n)
	elevationGains := make([]float64, n)
	calories := make([]float64, n)
	averageHRs := make([]float64, n)
	maxHRs := make([]float64, n)
	averageSpeeds := make([]float64, n)
	for i, a := range activities {
		ids[i] = a.ActivityID
		names[i] = a.ActivityName
		types[i] = a.ActivityType.TypeKey
		starts[i], _ = time.Parse("2006-01-02 15:04:05", a.StartTimeGMT)
		distances[i] = a.Distance
		durations[i] = a.Duration
		movingDurations[i] = a.MovingDuration
		elevationGains[i] = a.ElevationGain
		calories[i] = a.Calories
		averageHRs[i] = a.AverageHR
		maxHRs[i] = a.MaxHR
		averageSpeeds[i] = a.AverageSpeed
	}

	frame := data.NewFrame("activities",
		data.NewField("id", nil, ids),
		data.NewField("name", nil, names),
		data.NewField("type", nil, types),
		data.NewField("time", nil, starts),
		data.NewField("distance", nil, distances),
		data.NewField("duration", nil, durations),
		data.NewField("moving_duration", nil, movingDurations),
		data.NewField("elevation_gain", nil, elevationGains),
		data.NewField("calories", nil, calories),
		data.NewField("average_hr", nil, averageHRs),
		data.NewField("max_hr", nil, maxHRs),
		data.NewField("average_speed", nil, averageSpeeds),
	)
	units := map[int]string{4: "lengthm", 5: "s", 6: "s", 7: "lengthm", 11: "velocityms"}
	for i, u := range units {
		frame.Fields[i].Config = &data.FieldConfig{Unit: u}
	}
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}

	d.cachePut(key, frame, rangeTTL(timeRange))
	return frameResponse(frame)
}

func (d *Datasource) queryMetric(ctx context.Context, qm queryModel, timeRange backend.TimeRange) backend.DataResponse {
	def, ok := metricDefs[qm.Metric]
	if !ok {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("unknown metric %q", qm.Metric))
	}

	key := "metric|" + qm.Metric + "|" + dayRange(timeRange)
	if frame, ok := d.cacheGet(key); ok {
		return frameResponse(frame)
	}

	return d.coalesce(key, func() backend.DataResponse {
		return d.fetchMetric(ctx, key, def, qm, timeRange)
	})
}

func (d *Datasource) fetchMetric(ctx context.Context, key string, def metricDef, qm queryModel, timeRange backend.TimeRange) backend.DataResponse {
	client, err := d.garminClient(ctx)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, err.Error())
	}

	fetchCtx, span := startSpan(ctx, "garmin.metric", attribute.String("metric", qm.Metric))
	defer func() { span.End() }()

	var frame *data.Frame
	if def.fetchFrame != nil {
		frame, err = def.fetchFrame(fetchCtx, client, timeRange.From, timeRange.To)
		if err != nil {
			span.RecordError(err)
			return errDownstream("fetch %s: %v", qm.Metric, err)
		}
	} else {
		points, err := def.fetch(fetchCtx, client, timeRange.From, timeRange.To)
		if err != nil {
			span.RecordError(err)
			return errDownstream("fetch %s: %v", qm.Metric, err)
		}
		times := make([]time.Time, len(points))
		values := make([]float64, len(points))
		for i, p := range points {
			times[i] = p.t
			values[i] = p.v
		}
		frame = data.NewFrame(qm.Metric,
			data.NewField("time", nil, times),
			data.NewField(qm.Metric, nil, values),
		)
		if def.unit != "" {
			frame.Fields[1].Config = &data.FieldConfig{Unit: def.unit}
		}
	}
	log.DefaultLogger.FromContext(ctx).Debug("Fetched metric", "metric", qm.Metric, "rows", frame.Rows())

	d.cachePut(key, frame, rangeTTL(timeRange))
	return frameResponse(frame)
}

func (d *Datasource) queryTrack(ctx context.Context, qm queryModel) backend.DataResponse {
	if qm.ActivityID == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "activity id is required for track queries")
	}
	id, err := strconv.ParseInt(qm.ActivityID, 10, 64)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid activity id %q", qm.ActivityID))
	}

	points, err := d.trackPoints(ctx, id)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, err.Error())
	}

	n := len(points)
	times := make([]time.Time, n)
	lats := make([]float64, n)
	lons := make([]float64, n)
	eles := make([]*float64, n)
	hrs := make([]*float64, n)
	speeds := make([]*float64, n)
	distances := make([]float64, n)
	total := 0.0
	for i, p := range points {
		times[i] = p.Time
		lats[i] = p.Lat
		lons[i] = p.Lon
		eles[i] = p.Ele
		hrs[i] = p.HR
		if i > 0 {
			prev := points[i-1]
			step := haversineMeters(prev.Lat, prev.Lon, p.Lat, p.Lon)
			total += step
			if dt := p.Time.Sub(prev.Time).Seconds(); dt > 0 {
				v := step / dt
				speeds[i] = &v
			}
		}
		distances[i] = total
	}

	frame := data.NewFrame("track",
		data.NewField("time", nil, times),
		data.NewField("lat", nil, lats),
		data.NewField("lon", nil, lons),
		data.NewField("elevation", nil, eles),
		data.NewField("heartrate", nil, hrs),
		data.NewField("speed", nil, speeds),
		data.NewField("distance", nil, distances),
	)
	frame.Fields[3].Config = &data.FieldConfig{Unit: "lengthm"}
	frame.Fields[5].Config = &data.FieldConfig{Unit: "velocityms"}
	frame.Fields[6].Config = &data.FieldConfig{Unit: "lengthm"}

	var response backend.DataResponse
	response.Frames = append(response.Frames, frame)
	return response
}

// trackPoints returns the parsed GPX trackpoints for an activity, downloading
// them once and serving repeat dashboard refreshes from cache. Tracks are
// immutable once an activity is recorded, so entries never expire.
func (d *Datasource) trackPoints(ctx context.Context, id int64) ([]trackPoint, error) {
	d.trackMu.Lock()
	points, ok := d.tracks[id]
	d.trackMu.Unlock()
	if ok {
		return points, nil
	}

	v, err, _ := d.group.Do(fmt.Sprintf("track|%d", id), func() (any, error) {
		return d.fetchTrackPoints(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.([]trackPoint), nil
}

func (d *Datasource) fetchTrackPoints(ctx context.Context, id int64) ([]trackPoint, error) {
	client, err := d.garminClient(ctx)
	if err != nil {
		return nil, err
	}
	spanCtx, span := startSpan(ctx, "garmin.download_activity", attribute.Int64("activity_id", id))
	raw, err := client.DownloadActivity(spanCtx, id, garminconnect.FormatGPX)
	endSpan(span, err)
	if err != nil {
		return nil, fmt.Errorf("download activity %d: %w", id, err)
	}
	points, err := parseGPX(raw)
	if err != nil {
		return nil, err
	}
	log.DefaultLogger.FromContext(ctx).Debug("Downloaded activity track", "activityID", id, "bytes", len(raw), "points", len(points))

	d.trackMu.Lock()
	if len(d.tracks) >= maxCachedTracks {
		d.tracks = map[int64][]trackPoint{}
	}
	d.tracks[id] = points
	d.trackMu.Unlock()
	return points, nil
}
