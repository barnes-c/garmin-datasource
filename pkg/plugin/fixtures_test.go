package plugin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	garminconnect "github.com/barnes-c/go-garminconnect/garminconnect"

	"github.com/barnesc/garminconnect/pkg/models"
)

// fixtureServer routes Garmin API paths to canned JSON and counts requests
// per route so tests can assert cache and coalescing behavior.
type fixtureServer struct {
	*httptest.Server
	hits map[string]*atomic.Int64
}

func (f *fixtureServer) count(route string) int64 {
	if c, ok := f.hits[route]; ok {
		return c.Load()
	}
	return 0
}

// newFixtureServer serves the given route → body table, matching routes by
// path prefix.
func newFixtureServer(t *testing.T, routes map[string]string) *fixtureServer {
	t.Helper()
	f := &fixtureServer{hits: map[string]*atomic.Int64{}}
	for route := range routes {
		f.hits[route] = &atomic.Int64{}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for route, body := range routes {
			if strings.HasPrefix(r.URL.Path, route) {
				f.hits[route].Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
				return
			}
		}
		t.Errorf("unexpected request: %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(f.Close)
	return f
}

// newTestDatasource returns a Datasource whose client talks to the fixture
// server, bypassing login.
func newTestDatasource(f *fixtureServer, settings models.PluginSettings) *Datasource {
	if settings.Secrets == nil {
		settings.Secrets = &models.SecretPluginSettings{}
	}
	return &Datasource{
		settings:   &settings,
		client:     garminconnect.NewClient("", garminconnect.WithBaseURL(f.URL), garminconnect.WithToken("test-token")),
		frameCache: map[string]cachedFrame{},
	}
}

const activitiesFixture = `[
  {"activityId": 1, "activityName": "Morning Ride", "activityType": {"typeKey": "cycling"},
   "startTimeGMT": "2026-07-01 06:00:00", "duration": 3600, "elapsedDuration": 3700,
   "distance": 30000, "elevationGain": 250, "calories": 800, "averageHR": 130, "maxHR": 170, "averageSpeed": 8.333,
   "startLatitude": 46.2409, "startLongitude": 6.0299, "endLatitude": 46.2427, "endLongitude": 6.0247},
  {"activityId": 2, "activityName": "Evening Run", "activityType": {"typeKey": "running"},
   "startTimeGMT": "2026-07-02 18:00:00", "duration": 1800, "elapsedDuration": 0,
   "distance": 5000, "elevationGain": 40, "calories": 300, "averageHR": 150, "maxHR": 185, "averageSpeed": 2.778}
]`

const splitsWithLapsFixture = `{"activityId": 1, "splitSummaries": [], "lapDTOs": [
  {"startTimeGMT": "2026-07-01T06:00:00.0", "distance": 5000, "duration": 700, "averageSpeed": 7.1, "averageHR": 120, "maxHR": 130},
  {"startTimeGMT": "2026-07-01T06:11:40.0", "distance": 5000, "duration": 690, "averageSpeed": 7.2, "averageHR": 125, "maxHR": 140}
]}`

const trackGPXFixture = `<?xml version="1.0" encoding="UTF-8"?>
<gpx xmlns="http://www.topografix.com/GPX/1/1" xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">
  <trk><trkseg>
    <trkpt lat="46.2000" lon="6.1000"><ele>400</ele><time>2026-07-01T06:00:00Z</time>
      <extensions><gpxtpx:TrackPointExtension><gpxtpx:hr>120</gpxtpx:hr></gpxtpx:TrackPointExtension></extensions></trkpt>
    <trkpt lat="46.2010" lon="6.1000"><ele>405</ele><time>2026-07-01T06:00:10Z</time>
      <extensions><gpxtpx:TrackPointExtension><gpxtpx:hr>125</gpxtpx:hr></gpxtpx:TrackPointExtension></extensions></trkpt>
    <trkpt lat="46.2020" lon="6.1000"><ele>410</ele><time>2026-07-01T06:00:20Z</time>
      <extensions><gpxtpx:TrackPointExtension><gpxtpx:hr>130</gpxtpx:hr></gpxtpx:TrackPointExtension></extensions></trkpt>
  </trkseg></trk>
</gpx>`
