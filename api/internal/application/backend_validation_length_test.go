package application

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func repeatedName(runes int) string {
	return strings.Repeat("ñ", runes)
}

type nameLengthCase struct {
	label    string
	field    string
	validate func(string) error
}

func nameLengthCases() []nameLengthCase {
	return []nameLengthCase{
		{"talk title", "title", func(value string) error {
			input := validTalkInput()
			input.Title = value
			return ValidateTalkInput(input)
		}},
		{"talk location", "location", func(value string) error {
			input := validTalkInput()
			input.Location = value
			return ValidateTalkInput(input)
		}},
		{"competition name", "name", func(value string) error {
			input := validCompetitionInput()
			input.Name = value
			return ValidateCompetitionInput(input)
		}},
		{"competition location", "location", func(value string) error {
			input := validCompetitionInput()
			input.Location = value
			return ValidateCompetitionInput(input)
		}},
		{"team name", "name", func(value string) error {
			input := validTeamInput()
			input.Name = &value
			return ValidateTeamInput(input)
		}},
	}
}

func TestValidateAcceptsNamesOfTwoHundredCodePoints(t *testing.T) {
	for _, tc := range nameLengthCases() {
		t.Run(tc.label, func(t *testing.T) {
			if err := tc.validate(repeatedName(maxNameLength)); err != nil {
				t.Fatalf("%s of %d code points rejected: %v", tc.label, maxNameLength, err)
			}
		})
	}
}

func TestValidateRejectsNamesOverTwoHundredCodePoints(t *testing.T) {
	for _, tc := range nameLengthCases() {
		t.Run(tc.label, func(t *testing.T) {
			fields := validationFields(t, tc.validate(repeatedName(maxNameLength+1)))
			want := tc.field + " must be at most 200 characters"
			if fields[tc.field] != want {
				t.Fatalf("%s fields %v, want %q", tc.label, fields, want)
			}
		})
	}
}

func TestValidateAcceptsLongDescriptions(t *testing.T) {
	description := strings.Repeat("d", 6000)
	talk := validTalkInput()
	talk.Description = description
	if err := ValidateTalkInput(talk); err != nil {
		t.Fatalf("talk description of %d characters rejected: %v", len(description), err)
	}
	competition := validCompetitionInput()
	competition.Description = description
	if err := ValidateCompetitionInput(competition); err != nil {
		t.Fatalf("competition description of %d characters rejected: %v", len(description), err)
	}
}

type nameLengthRepository struct {
	BackendRepository
	calls int
}

func (r *nameLengthRepository) UpdateCompetition(context.Context, uuid.UUID, domain.CompetitionPatch) (domain.Competition, error) {
	r.calls++
	return domain.Competition{}, nil
}

func TestPatchesRejectNamesOverTwoHundredCodePoints(t *testing.T) {
	repo := &nameLengthRepository{}
	tooLong := repeatedName(maxNameLength + 1)
	nullableName := domain.NullableText{Value: &tooLong}
	cases := []struct {
		label string
		field string
		apply func() error
	}{
		{"talk patch", "title", func() error {
			_, err := ApplyTalkPatch(validTalkInput(), domain.TalkPatch{Title: &tooLong})
			return err
		}},
		{"competition patch", "name", func() error {
			_, err := ApplyCompetitionPatch(validCompetitionInput(), domain.CompetitionPatch{Name: &tooLong})
			return err
		}},
		{"team patch", "name", func() error {
			_, err := ApplyTeamPatch(validTeamInput(), domain.TeamPatch{Name: &nullableName})
			return err
		}},
		{"competition update", "name", func() error {
			_, err := NewBackend(repo).UpdateCompetition(context.Background(), uuid.New(),
				domain.CompetitionPatch{Name: &tooLong})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			fields := validationFields(t, tc.apply())
			want := tc.field + " must be at most 200 characters"
			if fields[tc.field] != want {
				t.Fatalf("%s fields %v, want %q", tc.label, fields, want)
			}
		})
	}
	if repo.calls != 0 {
		t.Fatalf("rejected competition update reached the repository %d times", repo.calls)
	}
}
