package application

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const maxTeamSize = 32767

const maxNameLength = 200

func ValidatePage(page domain.Page) error {
	if page.Limit < 1 || page.Limit > domain.MaxPageLimit || page.Offset < 0 {
		return domain.ErrInvalidPagination
	}
	return nil
}

func ValidateTalkFilter(filter domain.TalkFilter) error {
	switch filter.Period {
	case domain.TalkPeriodAll, domain.TalkPeriodUpcoming, domain.TalkPeriodPast:
		return nil
	default:
		return domain.ValidationError{Fields: map[string]string{"period": "period must be all, upcoming or past"}}
	}
}

func ValidateTalkInput(input domain.TalkInput) error {
	fields := map[string]string{}
	requireName(fields, "title", input.Title)
	requireText(fields, "description", input.Description)
	requireName(fields, "location", input.Location)
	if input.Language != "es" && input.Language != "en" {
		fields["language"] = "language must be es or en"
	}
	start := requireEventDate(fields, input.Date)
	if input.Duration < 1 || input.Duration > domain.MaxTalkDurationSeconds {
		fields["duration"] = "duration must be a positive number of seconds within the calendar range"
	} else if !start.IsZero() {
		if year := start.Add(time.Duration(input.Duration) * time.Second).Year(); year < 1 || year > 9999 {
			fields["duration"] = "duration overflows the representable calendar range"
		}
	}
	if input.StreamURL != nil && !validHTTPURL(*input.StreamURL) {
		fields["stream_url"] = "stream_url must be an http or https URL"
	}
	if input.PosterURL != nil && !validHTTPURL(*input.PosterURL) {
		fields["poster_url"] = "poster_url must be an http or https URL"
	}
	if !validIDs(input.SpeakerIDs) {
		fields["speaker_ids"] = "speaker_ids must contain at least one distinct person id"
	}
	return domain.NewValidationError(fields)
}

func ValidateCompetitionInput(input domain.CompetitionInput) error {
	fields := map[string]string{}
	requireName(fields, "name", input.Name)
	requireText(fields, "description", input.Description)
	requireName(fields, "location", input.Location)
	requireEventDate(fields, input.Date)
	if input.TeamSize < 1 || input.TeamSize > maxTeamSize {
		fields["team_size"] = "team_size must be between 1 and 32767"
	}
	return domain.NewValidationError(fields)
}

func ValidateTeamInput(input domain.TeamInput) error {
	fields := map[string]string{}
	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			fields["name"] = "name must not be blank"
		} else if !domain.ValidText(*input.Name) {
			fields["name"] = "name contains invalid text"
		} else if utf8.RuneCountInString(*input.Name) > maxNameLength {
			fields["name"] = "name must be at most 200 characters"
		}
	}
	if input.PhotoURL != nil && !validHTTPURL(*input.PhotoURL) {
		fields["photo_url"] = "photo_url must be an http or https URL"
	}
	if input.Position != nil && (*input.Position < 1 || *input.Position > maxTeamSize) {
		fields["position"] = "position must be between 1 and 32767"
	}
	if !validIDs(input.MemberIDs) {
		fields["member_ids"] = "member_ids must contain at least one distinct person id"
	}
	return domain.NewValidationError(fields)
}

func ValidateRole(role string) error {
	if role != "member" && role != "admin" {
		return domain.ValidationError{Fields: map[string]string{"role": "role must be member or admin"}}
	}
	return nil
}

func requireEventDate(fields map[string]string, date time.Time) time.Time {
	if date.IsZero() {
		fields["date"] = "date is required"
		return time.Time{}
	}
	instant, err := domain.EventInstant(date)
	if err != nil {
		fields["date"] = "date must be a valid Europe/Madrid local time"
		return time.Time{}
	}
	return instant
}

func requireText(fields map[string]string, name, value string) {
	if strings.TrimSpace(value) == "" {
		fields[name] = name + " is required"
		return
	}
	if !domain.ValidText(value) {
		fields[name] = name + " contains invalid text"
	}
}

func requireName(fields map[string]string, name, value string) {
	requireText(fields, name, value)
	if utf8.RuneCountInString(value) > maxNameLength {
		fields[name] = name + " must be at most 200 characters"
	}
}

func validIDs(ids []uuid.UUID) bool {
	if len(ids) == 0 {
		return false
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func validHTTPURL(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\n\r\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	return true
}
