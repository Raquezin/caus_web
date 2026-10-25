package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"caus-platform/api/internal/domain"
)

const calendarProductID = "-//CAUS//Talks Calendar//EN"
const calendarTimeLayout = "20060102T150405Z"
const calendarLineOctets = 75

var errCalendarEvent = errors.New("talk cannot be represented as a calendar event")

func (b *Backend) talksCalendar(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	talks, err := b.service.CalendarTalks(ctx)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	feed, err := buildCalendar(talks)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(feed))
}

func buildCalendar(talks []domain.Talk) (string, error) {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + calendarProductID}
	for _, talk := range talks {
		event, err := calendarEvent(talk)
		if err != nil {
			return "", err
		}
		lines = append(lines, event...)
	}
	lines = append(lines, "END:VCALENDAR")
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(foldCalendarLine(line))
	}
	return builder.String(), nil
}

func calendarEvent(talk domain.Talk) ([]string, error) {
	if talk.Duration <= 0 || talk.Duration > domain.MaxTalkDurationSeconds {
		return nil, errCalendarEvent
	}
	if !domain.ValidText(talk.Title) || !domain.ValidText(talk.Description) || !domain.ValidText(talk.Location) {
		return nil, errCalendarEvent
	}
	start, err := domain.EventInstant(talk.Date)
	if err != nil {
		return nil, err
	}
	end := start.Add(time.Duration(talk.Duration) * time.Second)
	if !validCalendarYear(end.Year()) {
		return nil, errCalendarEvent
	}
	stamp := talk.UpdatedAt.UTC()
	if !validCalendarYear(stamp.Year()) {
		return nil, errCalendarEvent
	}
	return []string{
		"BEGIN:VEVENT",
		"UID:" + talk.ID.String() + "@caus",
		"DTSTAMP:" + stamp.Format(calendarTimeLayout),
		"DTSTART:" + start.Format(calendarTimeLayout),
		"DTEND:" + end.Format(calendarTimeLayout),
		"SUMMARY:" + escapeCalendarText(talk.Title),
		"DESCRIPTION:" + escapeCalendarText(talk.Description),
		"LOCATION:" + escapeCalendarText(talk.Location),
		"END:VEVENT",
	}, nil
}

func validCalendarYear(year int) bool {
	return year >= 1 && year <= 9999
}

func escapeCalendarText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\\':
			builder.WriteString(`\\`)
		case ';':
			builder.WriteString(`\;`)
		case ',':
			builder.WriteString(`\,`)
		case '\r':
			if index+1 < len(value) && value[index+1] == '\n' {
				index++
			}
			builder.WriteString(`\n`)
		case '\n':
			builder.WriteString(`\n`)
		default:
			builder.WriteByte(value[index])
		}
	}
	return builder.String()
}

func foldCalendarLine(line string) string {
	chunks := make([]string, 0, len(line)/calendarLineOctets+1)
	remaining := line
	for len(remaining) > 0 {
		limit := calendarLineOctets
		if len(chunks) > 0 {
			limit--
		}
		cut := limit
		if cut >= len(remaining) {
			cut = len(remaining)
		} else {
			for cut > 0 && !utf8.RuneStart(remaining[cut]) {
				cut--
			}
		}
		chunks = append(chunks, remaining[:cut])
		remaining = remaining[cut:]
	}
	if len(chunks) == 0 {
		return "\r\n"
	}
	return strings.Join(chunks, "\r\n ") + "\r\n"
}
