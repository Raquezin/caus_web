package httpapi

import (
	"testing"
	"time"
)

func TestMadridZoneSeasonalOffsets(t *testing.T) {
	location := madridLocation()
	cases := []struct {
		season string
		date   time.Time
		zone   string
		offset int
	}{
		{"winter", time.Date(2026, 1, 15, 12, 0, 0, 0, location), "CET", 3600},
		{"summer", time.Date(2026, 7, 15, 12, 0, 0, 0, location), "CEST", 7200},
	}
	for _, testCase := range cases {
		zone, offset := testCase.date.Zone()
		if zone != testCase.zone || offset != testCase.offset {
			t.Fatalf("%s zone %q offset %d, want %q offset %d", testCase.season, zone, offset, testCase.zone, testCase.offset)
		}
	}
}

func TestMadridZoneFollowsDSTTransitions(t *testing.T) {
	location := madridLocation()
	cases := []struct {
		at     time.Time
		offset int
	}{
		{time.Date(2026, 3, 29, 1, 59, 0, 0, location), 3600},
		{time.Date(2026, 3, 29, 3, 0, 0, 0, location), 7200},
		{time.Date(2026, 10, 25, 1, 59, 0, 0, location), 7200},
		{time.Date(2026, 10, 25, 3, 0, 0, 0, location), 3600},
	}
	for _, testCase := range cases {
		if _, offset := testCase.at.Zone(); offset != testCase.offset {
			t.Fatalf("%s offset %d, want %d", testCase.at.Format(time.RFC3339), offset, testCase.offset)
		}
	}
}

func TestMadridNowTracksSeasonalOffset(t *testing.T) {
	winter := naiveMadridNow(time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC))
	summer := naiveMadridNow(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	if winter.Hour() != 11 || winter.Location() != time.UTC {
		t.Fatalf("winter naive now %v", winter)
	}
	if summer.Hour() != 12 || summer.Location() != time.UTC {
		t.Fatalf("summer naive now %v", summer)
	}
	if summer.Hour()-winter.Hour() != 1 {
		t.Fatalf("seasonal offset difference %d, want 1h", summer.Hour()-winter.Hour())
	}
}
