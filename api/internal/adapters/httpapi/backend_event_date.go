package httpapi

import (
	"fmt"
	"time"

	"caus-platform/api/internal/domain"
)

const eventDateLayout = domain.EventDateLayout

func madridLocation() *time.Location {
	return domain.MadridLocation()
}

func readEventDate(value string) (time.Time, error) {
	local, err := time.ParseInLocation(eventDateLayout, value, madridLocation())
	if err != nil {
		return time.Time{}, err
	}
	if local.Format(eventDateLayout) != value {
		return time.Time{}, fmt.Errorf("event date %q is not a valid local time", value)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC), nil
}

func formatEventDate(value time.Time) string {
	return value.Format(eventDateLayout)
}

func naiveMadridNow(now time.Time) time.Time {
	local := now.In(madridLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)
}
