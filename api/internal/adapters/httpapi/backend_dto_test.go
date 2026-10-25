package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestReadEventDate(t *testing.T) {
	for _, value := range []string{"2026-03-14T09:30:00", "2026-07-01T12:00:00", "2026-10-25T02:30:00"} {
		date, err := readEventDate(value)
		if err != nil {
			t.Fatalf("%s rejected: %v", value, err)
		}
		if date.Location() != time.UTC || formatEventDate(date) != value {
			t.Fatalf("%s round trip %v", value, date)
		}
	}
	date, err := readEventDate("2026-07-01T12:00:00")
	if err != nil || date.Hour() != 12 || date.Day() != 1 || date.Month() != time.July {
		t.Fatalf("wall clock components lost: %v %v", date, err)
	}
	for _, value := range []string{
		"2026-03-14T09:30:00+01:00", "2026-03-14T09:30:00Z", "2026-03-29T02:30:00",
		"2026-02-30T10:00:00", "2026-03-14 09:30:00", "2026-03-14T09:30:00.5", "not-a-date", "",
	} {
		if _, err := readEventDate(value); err == nil {
			t.Fatalf("%q accepted", value)
		}
	}
}

func TestNaiveMadridNow(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	naive := naiveMadridNow(now)
	if naive.Location() != time.UTC || naive.Hour() != 12 || naive.Day() != 1 {
		t.Fatalf("naive now %v", naive)
	}
	winter := naiveMadridNow(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	if winter.Hour() != 11 {
		t.Fatalf("winter naive now %v", winter)
	}
}

func TestTalkResponseMapping(t *testing.T) {
	streak := "https://example.com/stream"
	speaker := domain.Person{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}
	talk := domain.Talk{
		ID:          uuid.New(),
		Title:       "Title",
		Description: "Description",
		Location:    "Room",
		Language:    "en",
		Date:        time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Duration:    5400,
		StreamURL:   &streak,
		Published:   true,
		Speakers:    []domain.Person{speaker},
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}
	encoded, err := json.Marshal(talkResponse(talk))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["date"] != "2026-03-14T09:30:00" || decoded["duration"] != float64(5400) ||
		decoded["created_at"] != "2026-01-02T03:04:05Z" || decoded["poster_url"] != nil {
		t.Fatalf("talk response %s", encoded)
	}
	speakers, ok := decoded["speakers"].([]any)
	if !ok || len(speakers) != 1 || speakers[0].(map[string]any)["last_name"] != "Lovelace" {
		t.Fatalf("speakers %s", encoded)
	}
	empty, err := json.Marshal(talkResponses(nil))
	if err != nil || string(empty) != "[]" {
		t.Fatalf("empty talk list %s %v", empty, err)
	}
}

func TestTeamCompetitionUserAndExportMapping(t *testing.T) {
	competitionID := uuid.New()
	team := domain.Team{ID: uuid.New(), CompetitionID: competitionID, Members: []domain.Person{}, CreatedAt: time.Unix(0, 0).UTC()}
	detail, err := json.Marshal(competitionDetailResponse(domain.CompetitionDetail{
		Competition: domain.Competition{ID: competitionID, Name: "Spring", Date: time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC), TeamSize: 3},
		Teams:       []domain.Team{team},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Competition map[string]any   `json:"competition"`
		Teams       []map[string]any `json:"teams"`
	}
	if err := json.Unmarshal(detail, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Competition["date"] != "2026-05-02T18:00:00" || decoded.Competition["team_size"] != float64(3) {
		t.Fatalf("competition detail %s", detail)
	}
	if len(decoded.Teams) != 1 || decoded.Teams[0]["name"] != nil || decoded.Teams[0]["position"] != nil {
		t.Fatalf("team detail %s", detail)
	}
	user, err := json.Marshal(userResponse(domain.User{ID: uuid.New(), FirstName: "Ada", Role: "admin"}))
	if err != nil || !strings.Contains(string(user), `"email":null`) {
		t.Fatalf("user response %s %v", user, err)
	}
	account, err := json.Marshal(accountResponse(domain.Account{ID: uuid.New(), Email: "ada@example.com", Role: "member"}))
	if err != nil || !strings.Contains(string(account), `"email":"ada@example.com"`) {
		t.Fatalf("account response %s %v", account, err)
	}
	if strings.Contains(string(account), "google_sub") || strings.Contains(string(account), "token") {
		t.Fatalf("account response exposes private fields: %s", account)
	}
	export, err := json.Marshal(exportResponse(domain.AccountExport{GoogleSub: "subject"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"sessions":[]`, `"talks":[]`, `"teams":[]`, `"competitions":[]`, `"google_sub":"subject"`} {
		if !strings.Contains(string(export), field) {
			t.Fatalf("export missing %s: %s", field, export)
		}
	}
}

func TestParseTalkFilter(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/talks", nil)
	filter, ok := parseTalkFilter(request, true)
	if !ok || filter.Period != domain.TalkPeriodAll || !filter.IncludeUnpublished {
		t.Fatalf("default filter %+v %v", filter, ok)
	}
	if filter.Now.Location() != time.UTC || filter.Now.Sub(naiveMadridNow(time.Now())) > 2*time.Second ||
		naiveMadridNow(time.Now()).Sub(filter.Now) > 2*time.Second {
		t.Fatalf("filter now %v", filter.Now)
	}
	for _, period := range []string{"all", "upcoming", "past"} {
		request := httptest.NewRequest(http.MethodGet, "/api/talks?period="+period, nil)
		filter, ok := parseTalkFilter(request, false)
		if !ok || filter.Period != period {
			t.Fatalf("period %s: %+v %v", period, filter, ok)
		}
	}
	for _, rawQuery := range []string{"period=future", "period=", "period=all&period=past", "period=%zz"} {
		request := httptest.NewRequest(http.MethodGet, "/api/talks?"+rawQuery, nil)
		if _, ok := parseTalkFilter(request, false); ok {
			t.Fatalf("query %q accepted", rawQuery)
		}
	}
}

func TestParseUserQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	if search, ok := parseUserQuery(request); !ok || search != "" {
		t.Fatalf("empty query %q %v", search, ok)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q=%20Ada%20", nil)
	if search, ok := parseUserQuery(request); !ok || search != "Ada" {
		t.Fatalf("trimmed query %q %v", search, ok)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q=Ada&q=Grace", nil)
	if _, ok := parseUserQuery(request); ok {
		t.Fatal("duplicate query accepted")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q=%zz", nil)
	if _, ok := parseUserQuery(request); ok {
		t.Fatal("malformed query accepted")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q=%ff", nil)
	if _, ok := parseUserQuery(request); ok {
		t.Fatal("invalid utf-8 query accepted")
	}
	long := strings.Repeat("a", domain.MaxUserSearchLength+1)
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q="+long, nil)
	if _, ok := parseUserQuery(request); ok {
		t.Fatal("oversized query accepted")
	}
	request = httptest.NewRequest(http.MethodGet, "/api/admin/users?q="+long[:domain.MaxUserSearchLength], nil)
	if search, ok := parseUserQuery(request); !ok || len(search) != domain.MaxUserSearchLength {
		t.Fatalf("maximum query %d %v", len(search), ok)
	}
}
