package application

import (
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func invalidTextValues() []string {
	return []string{"a\x00b", "a\x01b", "a\x1fb", "a\x7fb", "a\xffb", "\uFEFF", "a\uFEFFb"}
}

func TestValidateTalkInputRejectsInvalidText(t *testing.T) {
	for _, field := range []string{"title", "description", "location"} {
		for _, value := range invalidTextValues() {
			input := validTalkInput()
			switch field {
			case "title":
				input.Title = value
			case "description":
				input.Description = value
			case "location":
				input.Location = value
			}
			if fields := validationFields(t, ValidateTalkInput(input)); fields[field] == "" {
				t.Fatalf("%s %q was not rejected", field, value)
			}
		}
	}
}

func TestValidateTalkInputAllowsMultibyteAndWhitespaceText(t *testing.T) {
	input := validTalkInput()
	input.Title = "Señales y Sistemas\t"
	input.Description = "línea uno\nlínea dos\r\nlínea tres"
	input.Location = "Sala Ω"
	if err := ValidateTalkInput(input); err != nil {
		t.Fatalf("legitimate text rejected: %v", err)
	}
}

func TestValidateCompetitionInputRejectsInvalidText(t *testing.T) {
	for _, field := range []string{"name", "description", "location"} {
		for _, value := range invalidTextValues() {
			input := validCompetitionInput()
			switch field {
			case "name":
				input.Name = value
			case "description":
				input.Description = value
			case "location":
				input.Location = value
			}
			if fields := validationFields(t, ValidateCompetitionInput(input)); fields[field] == "" {
				t.Fatalf("%s %q was not rejected", field, value)
			}
		}
	}
}

func TestValidateTeamInputRejectsInvalidTextName(t *testing.T) {
	for _, value := range invalidTextValues() {
		input := validTeamInput()
		name := value
		input.Name = &name
		if fields := validationFields(t, ValidateTeamInput(input)); fields["name"] == "" {
			t.Fatalf("name %q was not rejected", value)
		}
	}
}

func TestValidateTalkInputDurationBounds(t *testing.T) {
	for _, duration := range []int64{0, -1, domain.MaxTalkDurationSeconds + 1} {
		input := validTalkInput()
		input.Duration = duration
		if fields := validationFields(t, ValidateTalkInput(input)); fields["duration"] == "" {
			t.Fatalf("duration %d was not rejected", duration)
		}
	}
	for _, duration := range []int64{1, domain.MaxTalkDurationSeconds} {
		input := validTalkInput()
		input.Duration = duration
		if err := ValidateTalkInput(input); err != nil {
			t.Fatalf("duration %d rejected: %v", duration, err)
		}
	}
}

func TestValidateTalkInputDateBounds(t *testing.T) {
	cases := []struct {
		name  string
		date  time.Time
		field string
	}{
		{"spring gap", time.Date(2026, 3, 29, 2, 30, 0, 0, time.UTC), "date"},
		{"year zero", time.Date(0, 6, 1, 12, 0, 0, 0, time.UTC), "date"},
		{"year ten thousand", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "date"},
		{"end overflow", time.Date(9999, 12, 31, 23, 30, 0, 0, time.UTC), "duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := validTalkInput()
			input.Date = tc.date
			input.Duration = 7200
			if tc.field == "date" {
				input.Duration = 3600
			}
			if fields := validationFields(t, ValidateTalkInput(input)); fields[tc.field] == "" {
				t.Fatalf("date %v did not fail field %q", tc.date, tc.field)
			}
		})
	}
	input := validTalkInput()
	input.Date = time.Date(2026, 10, 25, 2, 30, 0, 0, time.UTC)
	if err := ValidateTalkInput(input); err != nil {
		t.Fatalf("ambiguous autumn hour rejected: %v", err)
	}
	input.Date = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	if err := ValidateTalkInput(input); err != nil {
		t.Fatalf("winter date rejected: %v", err)
	}
	input.Date = time.Date(1, 1, 1, 0, 0, 1, 0, time.UTC)
	if err := ValidateTalkInput(input); err != nil {
		t.Fatalf("year one wall clock rejected: %v", err)
	}
}
