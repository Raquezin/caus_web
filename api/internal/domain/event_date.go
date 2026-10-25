package domain

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const EventDateLayout = "2006-01-02T15:04:05"

var madridZone = loadMadridZone()

func loadMadridZone() *time.Location {
	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		panic(fmt.Sprintf("load Europe/Madrid: %v", err))
	}
	return location
}

func MadridLocation() *time.Location {
	return madridZone
}

func EventInstant(wallClock time.Time) (time.Time, error) {
	if year := wallClock.Year(); year < 1 || year > 9999 {
		return time.Time{}, fmt.Errorf("event year %d is out of range", year)
	}
	local := time.Date(wallClock.Year(), wallClock.Month(), wallClock.Day(), wallClock.Hour(),
		wallClock.Minute(), wallClock.Second(), 0, MadridLocation())
	if local.Format(EventDateLayout) != wallClock.Format(EventDateLayout) {
		return time.Time{}, fmt.Errorf("event time %s is not a valid Europe/Madrid local time",
			wallClock.Format(EventDateLayout))
	}
	instant := local.UTC()
	if year := instant.Year(); year < 1 || year > 9999 {
		return time.Time{}, fmt.Errorf("event instant year %d is out of range", year)
	}
	return instant, nil
}
