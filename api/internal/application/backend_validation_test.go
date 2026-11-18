package application

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func validationFields(t *testing.T, err error) map[string]string {
	t.Helper()
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %v is not a validation error", err)
	}
	return validation.Fields
}

func validTalkInput() domain.TalkInput {
	return domain.TalkInput{
		Title:       "Talk",
		Description: "Description",
		Location:    "Room 1",
		Language:    "es",
		Date:        time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Duration:    3600,
		SpeakerIDs:  []uuid.UUID{uuid.New()},
	}
}

func validCompetitionInput() domain.CompetitionInput {
	return domain.CompetitionInput{
		Name:        "Spring",
		Description: "Description",
		Location:    "Hall",
		Date:        time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC),
		TeamSize:    3,
	}
}

func validTeamInput() domain.TeamInput {
	return domain.TeamInput{MemberIDs: []uuid.UUID{uuid.New()}}
}

func TestValidateTalkInput(t *testing.T) {
	if err := ValidateTalkInput(validTalkInput()); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	cases := []struct {
		field  string
		mutate func(*domain.TalkInput)
	}{
		{"title", func(in *domain.TalkInput) { in.Title = "  " }},
		{"description", func(in *domain.TalkInput) { in.Description = "" }},
		{"location", func(in *domain.TalkInput) { in.Location = "" }},
		{"language", func(in *domain.TalkInput) { in.Language = "fr" }},
		{"date", func(in *domain.TalkInput) { in.Date = time.Time{} }},
		{"duration", func(in *domain.TalkInput) { in.Duration = 0 }},
		{"stream_url", func(in *domain.TalkInput) { value := "ftp://example.com"; in.StreamURL = &value }},
		{"poster_url", func(in *domain.TalkInput) { value := "javascript:alert(1)"; in.PosterURL = &value }},
		{"speaker_ids", func(in *domain.TalkInput) { in.SpeakerIDs = nil }},
	}
	for _, tc := range cases {
		input := validTalkInput()
		tc.mutate(&input)
		if fields := validationFields(t, ValidateTalkInput(input)); fields[tc.field] == "" {
			t.Fatalf("case %s: missing error in %v", tc.field, fields)
		}
	}
	duplicate := uuid.New()
	input := validTalkInput()
	input.SpeakerIDs = []uuid.UUID{duplicate, duplicate}
	if fields := validationFields(t, ValidateTalkInput(input)); fields["speaker_ids"] == "" {
		t.Fatal("duplicate speakers accepted")
	}
	input.SpeakerIDs = []uuid.UUID{uuid.Nil}
	if fields := validationFields(t, ValidateTalkInput(input)); fields["speaker_ids"] == "" {
		t.Fatal("nil speaker accepted")
	}
	for _, value := range []string{"https://example.com", "http://example.com/path?x=1#part", "https://example.com:080/", "HTTPS://EXAMPLE.COM/Path"} {
		input := validTalkInput()
		input.StreamURL = &value
		if err := ValidateTalkInput(input); err != nil {
			t.Fatalf("valid URL %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"ftp://example.com", "javascript:alert(1)", "https://user@example.com/", "https://example.com:99999", "https://example.com:0", "https://exa mple.com/", "", "example.com"} {
		input := validTalkInput()
		input.PosterURL = &value
		if fields := validationFields(t, ValidateTalkInput(input)); fields["poster_url"] == "" {
			t.Fatalf("invalid URL %q accepted", value)
		}
	}
}

func TestValidateCompetitionAndTeamInput(t *testing.T) {
	if err := ValidateCompetitionInput(validCompetitionInput()); err != nil {
		t.Fatalf("valid competition rejected: %v", err)
	}
	for _, size := range []int{0, -1, 32768} {
		input := validCompetitionInput()
		input.TeamSize = size
		if fields := validationFields(t, ValidateCompetitionInput(input)); fields["team_size"] == "" {
			t.Fatalf("team size %d accepted", size)
		}
	}
	if fields := validationFields(t, ValidateCompetitionInput(domain.CompetitionInput{})); len(fields) < 4 {
		t.Fatalf("empty competition fields %v", fields)
	}
	if err := ValidateTeamInput(validTeamInput()); err != nil {
		t.Fatalf("valid team rejected: %v", err)
	}
	blank := " "
	input := validTeamInput()
	input.Name = &blank
	if fields := validationFields(t, ValidateTeamInput(input)); fields["name"] == "" {
		t.Fatal("blank team name accepted")
	}
	for _, position := range []int{0, -2, 32768} {
		input := validTeamInput()
		input.Position = &position
		if fields := validationFields(t, ValidateTeamInput(input)); fields["position"] == "" {
			t.Fatalf("position %d accepted", position)
		}
	}
	input = validTeamInput()
	input.PhotoURL = &blank
	if fields := validationFields(t, ValidateTeamInput(input)); fields["photo_url"] == "" {
		t.Fatal("invalid photo url accepted")
	}
	input = validTeamInput()
	input.MemberIDs = []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	if err := ValidateTeamInput(input); err != nil {
		t.Fatalf("multiple members rejected: %v", err)
	}
}

func TestValidatePageAndFilter(t *testing.T) {
	if err := ValidatePage(domain.Page{Limit: 20}); err != nil {
		t.Fatalf("valid page rejected: %v", err)
	}
	for _, page := range []domain.Page{{Limit: 0}, {Limit: 101}, {Limit: 20, Offset: -1}} {
		if !errors.Is(ValidatePage(page), domain.ErrInvalidPagination) {
			t.Fatalf("page %+v accepted", page)
		}
	}
	for _, period := range []string{domain.TalkPeriodAll, domain.TalkPeriodUpcoming, domain.TalkPeriodPast} {
		if err := ValidateTalkFilter(domain.TalkFilter{Period: period}); err != nil {
			t.Fatalf("period %s rejected: %v", period, err)
		}
	}
	if fields := validationFields(t, ValidateTalkFilter(domain.TalkFilter{Period: "future"})); fields["period"] == "" {
		t.Fatal("unknown period accepted")
	}
	if err := ValidateRole("admin"); err != nil {
		t.Fatalf("admin role rejected: %v", err)
	}
	if fields := validationFields(t, ValidateRole("owner")); fields["role"] == "" {
		t.Fatal("unknown role accepted")
	}
}

func TestValidateCompetitionInputRejectsYearZeroDate(t *testing.T) {
	input := validCompetitionInput()
	input.Date = time.Date(0, 6, 1, 10, 0, 0, 0, time.UTC)
	if fields := validationFields(t, ValidateCompetitionInput(input)); fields["date"] == "" {
		t.Fatalf("year zero date accepted: %v", fields)
	}
}
