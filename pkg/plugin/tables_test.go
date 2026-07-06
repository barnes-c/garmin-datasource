package plugin

import (
	"testing"
	"time"

	"github.com/barnesc/garminconnect/pkg/models"
)

func prDatasource(unitSystem string) *Datasource {
	return &Datasource{settings: &models.PluginSettings{
		UnitSystem: unitSystem,
		Secrets:    &models.SecretPluginSettings{},
	}}
}

func TestFormatPRValue(t *testing.T) {
	d := prDatasource("")
	cases := []struct {
		typeID int64
		value  float64
		want   string
	}{
		{3, 1261.77, "21:02"},
		{11, 7106.2, "1:58:26"},
		{18, 61.876, "1:02"},
		{7, 18013.72, "18.01 km"},
		{9, 456, "456 m"},
		{17, 1700, "1700 m"},
		{10, 250, "250 W"},
		{12, 37589, "37589 steps"},
		{15, 8, "8 days"},
		{16, 2, "2 days"},
		{99, 7106.2021484375, "7106.2"},
	}
	for _, c := range cases {
		if got := d.formatPRValue(c.typeID, c.value); got != c.want {
			t.Errorf("formatPRValue(%d, %v) = %q, want %q", c.typeID, c.value, got, c.want)
		}
	}
}

func TestFormatPRValueImperial(t *testing.T) {
	d := prDatasource("imperial")
	cases := []struct {
		typeID int64
		value  float64
		want   string
	}{
		{7, 18013.72, "11.19 mi"},
		{9, 456, "1496 ft"},
		{17, 1700, "1700 m"}, // pools stay metric
		{3, 1261.77, "21:02"},
	}
	for _, c := range cases {
		if got := d.formatPRValue(c.typeID, c.value); got != c.want {
			t.Errorf("imperial formatPRValue(%d, %v) = %q, want %q", c.typeID, c.value, got, c.want)
		}
	}
}

func TestGmtTime(t *testing.T) {
	for _, s := range []string{
		"2026-07-01T06:00:00.0",
		"2026-07-01T06:00:00",
		"2026-07-01 06:00:00.0",
		"2026-07-01 06:00:00",
	} {
		got, ok := gmtTime(s)
		if !ok || !got.Equal(time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC)) {
			t.Errorf("gmtTime(%q) = %v, %v", s, got, ok)
		}
	}
	if _, ok := gmtTime("not a time"); ok {
		t.Error("expected failure for garbage input")
	}
}

func TestDay(t *testing.T) {
	if got, ok := day("2026-07-01"); !ok || got.Day() != 1 {
		t.Errorf("day() = %v, %v", got, ok)
	}
	if _, ok := day(""); ok {
		t.Error("expected failure for empty input")
	}
}

func TestHaversine(t *testing.T) {
	// One degree of latitude is ~111.2 km.
	got := haversineMeters(46.0, 6.0, 47.0, 6.0)
	if got < 110000 || got > 112500 {
		t.Errorf("expected ~111km, got %v", got)
	}
	if haversineMeters(46.0, 6.0, 46.0, 6.0) != 0 {
		t.Error("identical points must be 0")
	}
}
