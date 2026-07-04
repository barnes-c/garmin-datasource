package plugin

import (
	"encoding/xml"
	"fmt"
	"math"
	"time"
)

type trackPoint struct {
	Lat  float64   `xml:"lat,attr"`
	Lon  float64   `xml:"lon,attr"`
	Ele  *float64  `xml:"ele"`
	Time time.Time `xml:"time"`
	HR   *float64  `xml:"extensions>TrackPointExtension>hr"`
}

type gpxFile struct {
	Tracks []struct {
		Segments []struct {
			Points []trackPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
}

const earthRadiusMeters = 6371000

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(a))
}

// parseGPX flattens all tracks and segments of a GPX document into a single
// ordered point list.
func parseGPX(raw []byte) ([]trackPoint, error) {
	var g gpxFile
	if err := xml.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("parse gpx: %w", err)
	}
	var points []trackPoint
	for _, trk := range g.Tracks {
		for _, seg := range trk.Segments {
			points = append(points, seg.Points...)
		}
	}
	return points, nil
}
