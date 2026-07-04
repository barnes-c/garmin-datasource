package plugin

import (
	"context"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/barnesc/garminconnect/pkg/models"
)

func TestQueryData(t *testing.T) {
	ds := Datasource{
		settings: &models.PluginSettings{Secrets: &models.SecretPluginSettings{}},
		tracks:   map[int64][]trackPoint{},
	}

	resp, err := ds.QueryData(
		context.Background(),
		&backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{RefID: "A", QueryType: queryTypeTrack, JSON: []byte(`{}`)},
				{RefID: "B", QueryType: "bogus", JSON: []byte(`{}`)},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resp.Responses))
	}
	if resp.Responses["A"].Error == nil {
		t.Error("track query without activity id should fail")
	}
	if resp.Responses["B"].Error == nil {
		t.Error("unknown query type should fail")
	}
}

type captureSender struct {
	resp *backend.CallResourceResponse
}

func (s *captureSender) Send(resp *backend.CallResourceResponse) error {
	s.resp = resp
	return nil
}

func TestCallResourceMFAWithoutPendingLogin(t *testing.T) {
	ds := Datasource{
		settings: &models.PluginSettings{Secrets: &models.SecretPluginSettings{}},
		tracks:   map[int64][]trackPoint{},
	}

	sender := &captureSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   "mfa",
		Method: "POST",
		Body:   []byte(`{"code":"123456"}`),
	}, sender)
	if err != nil {
		t.Fatal(err)
	}
	if sender.resp.Status != 409 {
		t.Errorf("expected 409 without a pending login, got %d", sender.resp.Status)
	}

	err = ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Path:   "mfa",
		Method: "POST",
		Body:   []byte(`{}`),
	}, sender)
	if err != nil {
		t.Fatal(err)
	}
	if sender.resp.Status != 400 {
		t.Errorf("expected 400 for missing code, got %d", sender.resp.Status)
	}
}

const sampleGPX = `<?xml version="1.0" encoding="UTF-8"?>
<gpx xmlns="http://www.topografix.com/GPX/1/1"
     xmlns:gpxtpx="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">
  <trk>
    <name>Morning Run</name>
    <trkseg>
      <trkpt lat="48.2082" lon="16.3738">
        <ele>171.2</ele>
        <time>2026-07-01T06:00:00Z</time>
        <extensions>
          <gpxtpx:TrackPointExtension>
            <gpxtpx:hr>121</gpxtpx:hr>
          </gpxtpx:TrackPointExtension>
        </extensions>
      </trkpt>
      <trkpt lat="48.2085" lon="16.3741">
        <time>2026-07-01T06:00:05Z</time>
      </trkpt>
    </trkseg>
  </trk>
</gpx>`

func TestParseGPX(t *testing.T) {
	points, err := parseGPX([]byte(sampleGPX))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}

	p := points[0]
	if p.Lat != 48.2082 || p.Lon != 16.3738 {
		t.Errorf("unexpected coordinates: %v, %v", p.Lat, p.Lon)
	}
	if p.Ele == nil || *p.Ele != 171.2 {
		t.Errorf("unexpected elevation: %v", p.Ele)
	}
	if p.HR == nil || *p.HR != 121 {
		t.Errorf("unexpected heart rate: %v", p.HR)
	}
	if p.Time.IsZero() {
		t.Error("time not parsed")
	}

	if points[1].Ele != nil || points[1].HR != nil {
		t.Error("missing extension values should be nil")
	}
}
