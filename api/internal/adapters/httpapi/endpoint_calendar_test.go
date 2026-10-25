package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type calendarServiceStub struct {
	BackendService
	talks []domain.Talk
	err   error
	calls int
}

func (s *calendarServiceStub) CalendarTalks(context.Context) ([]domain.Talk, error) {
	s.calls++
	return s.talks, s.err
}

func serveCalendar(t *testing.T, service BackendService) (*httptest.ResponseRecorder, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	backend := NewBackend(service, nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	recorder := httptest.NewRecorder()
	backend.talksCalendar(recorder, httptest.NewRequest(http.MethodGet, "/api/talks.ics", nil))
	return recorder, &logs
}

func calendarValues(lines []string, prefix string) []string {
	values := make([]string, 0, 2)
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			values = append(values, line)
		}
	}
	return values
}

func calendarLogicalLines(t *testing.T, body string) []string {
	t.Helper()
	if !utf8.ValidString(body) {
		t.Fatal("calendar body is not valid UTF-8")
	}
	for index := 0; index < len(body); index++ {
		switch body[index] {
		case '\r':
			if index+1 >= len(body) || body[index+1] != '\n' {
				t.Fatalf("bare CR in calendar body: %q", body)
			}
		case '\n':
			if index == 0 || body[index-1] != '\r' {
				t.Fatalf("bare LF in calendar body: %q", body)
			}
		}
	}
	if !strings.HasSuffix(body, "\r\n") {
		t.Fatalf("calendar body does not end with CRLF: %q", body)
	}
	for _, line := range strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n") {
		if len(line) > calendarLineOctets {
			t.Fatalf("physical line %q is %d octets", line, len(line))
		}
		if !utf8.ValidString(line) {
			t.Fatalf("physical line splits a UTF-8 codepoint: %q", line)
		}
	}
	unfolded := strings.TrimSuffix(strings.ReplaceAll(body, "\r\n ", ""), "\r\n")
	lines := strings.Split(unfolded, "\r\n")
	for _, line := range lines {
		if line == "" {
			t.Fatalf("empty calendar logical line in %q", body)
		}
	}
	return lines
}

func TestEndpointCalendarMadridWallClock(t *testing.T) {
	winterID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	summerID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	winter := domain.Talk{
		ID: winterID, Title: "Winter", Description: "Winter talk", Location: "Madrid",
		Date:      time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Duration:  5400,
		UpdatedAt: time.Date(2026, 1, 2, 12, 30, 0, 0, time.FixedZone("CET", 3600)),
	}
	summer := domain.Talk{
		ID: summerID, Title: "Summer", Description: "Summer talk", Location: "Madrid",
		Date:      time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Duration:  3600,
		UpdatedAt: time.Date(2026, 7, 2, 12, 30, 0, 0, time.UTC),
	}
	recorder, _ := serveCalendar(t, &calendarServiceStub{talks: []domain.Talk{winter, summer}})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	if got := recorder.Result().Header.Get("Content-Type"); got != "text/calendar; charset=utf-8" {
		t.Fatalf("content type %q", got)
	}
	lines := calendarLogicalLines(t, recorder.Body.String())
	for _, want := range []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + calendarProductID,
		"UID:" + winterID.String() + "@caus", "DTSTAMP:20260102T113000Z",
		"DTSTART:20260115T090000Z", "DTEND:20260115T103000Z",
		"UID:" + summerID.String() + "@caus", "DTSTAMP:20260702T123000Z",
		"DTSTART:20260701T080000Z", "DTEND:20260701T090000Z", "END:VCALENDAR",
	} {
		if len(calendarValues(lines, want)) == 0 {
			t.Fatalf("missing %q in %q", want, lines)
		}
	}
	if count := len(calendarValues(lines, "BEGIN:VEVENT")); count != 2 {
		t.Fatalf("event count %d", count)
	}
}

func TestEndpointCalendarEscapesLineInjection(t *testing.T) {
	talk := domain.Talk{
		ID:          uuid.New(),
		Title:       "Line1\r\nATTENDEE:mailto:evil@example.com\rX\\Y,Z;W",
		Description: "First line\nSecond line",
		Location:    "Room;1,2",
		Date:        time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Duration:    3600,
		UpdatedAt:   time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
	}
	recorder, _ := serveCalendar(t, &calendarServiceStub{talks: []domain.Talk{talk}})
	body := recorder.Body.String()
	lines := calendarLogicalLines(t, body)
	for _, line := range lines {
		if strings.HasPrefix(line, "ATTENDEE:") {
			t.Fatalf("injected property line %q", line)
		}
	}
	if strings.Contains(body, "\r\nATTENDEE:") {
		t.Fatalf("injected CRLF property %q", body)
	}
	for prefix, want := range map[string]string{
		"SUMMARY:":     `SUMMARY:Line1\nATTENDEE:mailto:evil@example.com\nX\\Y\,Z\;W`,
		"DESCRIPTION:": `DESCRIPTION:First line\nSecond line`,
		"LOCATION:":    `LOCATION:Room\;1\,2`,
	} {
		values := calendarValues(lines, prefix)
		if len(values) != 1 || values[0] != want {
			t.Fatalf("%s values %q, want %q", prefix, values, want)
		}
	}
}

func TestEndpointCalendarFoldsLongMultibyteText(t *testing.T) {
	title := strings.Repeat("Taller de programación 日本語 ", 10)
	talk := domain.Talk{
		ID:          uuid.New(),
		Title:       title,
		Description: strings.Repeat("Descripción larga ", 10),
		Location:    "Madrid",
		Date:        time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Duration:    3600,
		UpdatedAt:   time.Date(2026, 7, 2, 12, 30, 0, 0, time.UTC),
	}
	recorder, _ := serveCalendar(t, &calendarServiceStub{talks: []domain.Talk{talk}})
	body := recorder.Body.String()
	lines := calendarLogicalLines(t, body)
	values := calendarValues(lines, "SUMMARY:")
	if len(values) != 1 || values[0] != "SUMMARY:"+escapeCalendarText(title) {
		t.Fatalf("summary %q", values)
	}
	continuations := 0
	for _, line := range strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n") {
		if strings.HasPrefix(line, " ") {
			continuations++
		}
	}
	if continuations == 0 {
		t.Fatal("long multibyte text was not folded")
	}
}

func TestEndpointCalendarEmptyFeed(t *testing.T) {
	for _, talks := range [][]domain.Talk{nil, {}} {
		recorder, _ := serveCalendar(t, &calendarServiceStub{talks: talks})
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d", recorder.Code)
		}
		lines := calendarLogicalLines(t, recorder.Body.String())
		want := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + calendarProductID, "END:VCALENDAR"}
		if strings.Join(lines, "|") != strings.Join(want, "|") {
			t.Fatalf("empty feed %q", lines)
		}
	}
}

func TestEndpointCalendarServiceFailureSanitized(t *testing.T) {
	recorder, logs := serveCalendar(t, &calendarServiceStub{err: errors.New("SECRET-MARKER calendar failure")})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	if strings.Contains(recorder.Body.String(), "SECRET-MARKER") || strings.Contains(logs.String(), "SECRET-MARKER") {
		t.Fatalf("failure leaked: body %q logs %q", recorder.Body.String(), logs.String())
	}
}

func TestEndpointCalendarRejectsInvalidEvents(t *testing.T) {
	valid := domain.Talk{
		ID:        uuid.New(),
		Title:     "Valid",
		Location:  "Madrid",
		Date:      time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Duration:  3600,
		UpdatedAt: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
	}
	invalidDuration := domain.MaxTalkDurationSeconds + 1
	cases := []struct {
		name string
		talk domain.Talk
	}{
		{"zero duration", domain.Talk{Date: valid.Date, Duration: 0, UpdatedAt: valid.UpdatedAt}},
		{"negative duration", domain.Talk{Date: valid.Date, Duration: -1, UpdatedAt: valid.UpdatedAt}},
		{"overflowing duration", domain.Talk{Date: valid.Date, Duration: invalidDuration, UpdatedAt: valid.UpdatedAt}},
		{"out of range start", domain.Talk{
			Date: time.Date(10000, 1, 1, 10, 0, 0, 0, time.UTC), Duration: 3600, UpdatedAt: valid.UpdatedAt,
		}},
		{"out of range stamp", domain.Talk{
			Date: valid.Date, Duration: 3600, UpdatedAt: time.Date(10000, 1, 1, 10, 0, 0, 0, time.UTC),
		}},
		{"madrid spring gap", domain.Talk{
			Date: time.Date(2026, 3, 29, 2, 30, 0, 0, time.UTC), Duration: 3600, UpdatedAt: valid.UpdatedAt,
		}},
		{"end year overflow", domain.Talk{
			Date: time.Date(9999, 12, 31, 23, 30, 0, 0, time.UTC), Duration: 7200, UpdatedAt: valid.UpdatedAt,
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder, _ := serveCalendar(t, &calendarServiceStub{talks: []domain.Talk{valid, testCase.talk}})
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status %d", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), "BEGIN:VCALENDAR") {
				t.Fatalf("partial calendar written: %q", recorder.Body.String())
			}
		})
	}
}

func TestEndpointCalendarRejectsForbiddenText(t *testing.T) {
	base := domain.Talk{
		ID:        uuid.New(),
		Title:     "Valid",
		Location:  "Madrid",
		Date:      time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Duration:  3600,
		UpdatedAt: time.Date(2026, 1, 2, 12, 30, 0, 0, time.UTC),
	}
	for _, field := range []string{"title", "description", "location"} {
		t.Run(field, func(t *testing.T) {
			for _, value := range []string{"a\x00b", "a\x01b", "a\x7fb", "a\xffb"} {
				talk := base
				switch field {
				case "title":
					talk.Title = value
				case "description":
					talk.Description = value
				case "location":
					talk.Location = value
				}
				recorder, _ := serveCalendar(t, &calendarServiceStub{talks: []domain.Talk{talk}})
				if recorder.Code != http.StatusInternalServerError {
					t.Fatalf("%s %q status %d", field, value, recorder.Code)
				}
				if strings.Contains(recorder.Body.String(), "BEGIN:VCALENDAR") {
					t.Fatalf("%s %q wrote calendar: %q", field, value, recorder.Body.String())
				}
			}
		})
	}
}
