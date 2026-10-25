package application

import (
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestApplyTalkPatch(t *testing.T) {
	current := validTalkInput()
	stream := "https://example.com/stream"
	poster := "https://example.com/poster"
	if _, err := ApplyTalkPatch(current, domain.TalkPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	speaker := uuid.New()
	title := "Updated"
	duration := int64(1800)
	published := false
	patch := domain.TalkPatch{
		Title:      &title,
		Duration:   &duration,
		Published:  &published,
		StreamURL:  &domain.NullableText{Value: &stream},
		PosterURL:  &domain.NullableText{Value: &poster},
		SpeakerIDs: &[]uuid.UUID{speaker},
	}
	merged, err := ApplyTalkPatch(current, patch)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Title != "Updated" || merged.Duration != 1800 || merged.Published || merged.StreamURL == nil ||
		merged.PosterURL == nil || len(merged.SpeakerIDs) != 1 || merged.SpeakerIDs[0] != speaker {
		t.Fatalf("merged talk %+v", merged)
	}
	if merged.Description != current.Description || !merged.Date.Equal(current.Date) {
		t.Fatalf("omitted fields changed: %+v", merged)
	}
	cleared, err := ApplyTalkPatch(current, domain.TalkPatch{StreamURL: &domain.NullableText{}, PosterURL: &domain.NullableText{}})
	if err != nil || cleared.StreamURL != nil || cleared.PosterURL != nil {
		t.Fatalf("nullable clear: %v %+v", err, cleared)
	}
	blank := " "
	if fields := validationFields(t, mustTalkPatchError(t, current, domain.TalkPatch{Title: &blank})); fields["title"] == "" {
		t.Fatal("blank title accepted")
	}
}

func mustTalkPatchError(t *testing.T, current domain.TalkInput, patch domain.TalkPatch) error {
	t.Helper()
	_, err := ApplyTalkPatch(current, patch)
	if err == nil {
		t.Fatal("patch accepted")
	}
	return err
}

func TestApplyCompetitionPatch(t *testing.T) {
	current := validCompetitionInput()
	if _, err := ApplyCompetitionPatch(current, domain.CompetitionPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	name := "Autumn"
	size := 5
	merged, err := ApplyCompetitionPatch(current, domain.CompetitionPatch{Name: &name, TeamSize: &size})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Name != "Autumn" || merged.TeamSize != 5 || merged.Location != current.Location {
		t.Fatalf("merged competition %+v", merged)
	}
	zero := 0
	if fields := validationFields(t, mustCompetitionPatchError(t, current, domain.CompetitionPatch{TeamSize: &zero})); fields["team_size"] == "" {
		t.Fatal("zero team size accepted")
	}
}

func mustCompetitionPatchError(t *testing.T, current domain.CompetitionInput, patch domain.CompetitionPatch) error {
	t.Helper()
	_, err := ApplyCompetitionPatch(current, patch)
	if err == nil {
		t.Fatal("patch accepted")
	}
	return err
}

func TestApplyTeamPatch(t *testing.T) {
	current := validTeamInput()
	if _, err := ApplyTeamPatch(current, domain.TeamPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	name := "Team A"
	position := 2
	member := uuid.New()
	merged, err := ApplyTeamPatch(current, domain.TeamPatch{
		Name:      &domain.NullableText{Value: &name},
		Position:  &domain.NullableInt{Value: &position},
		MemberIDs: &[]uuid.UUID{member},
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Name == nil || *merged.Name != "Team A" || merged.Position == nil || *merged.Position != 2 ||
		len(merged.MemberIDs) != 1 || merged.MemberIDs[0] != member {
		t.Fatalf("merged team %+v", merged)
	}
	cleared, err := ApplyTeamPatch(current, domain.TeamPatch{Name: &domain.NullableText{}, Position: &domain.NullableInt{}})
	if err != nil || cleared.Name != nil || cleared.Position != nil {
		t.Fatalf("nullable clear: %v %+v", err, cleared)
	}
	invalidPosition := 0
	if fields := validationFields(t, mustTeamPatchError(t, current, domain.TeamPatch{Position: &domain.NullableInt{Value: &invalidPosition}})); fields["position"] == "" {
		t.Fatal("zero position accepted")
	}
	if fields := validationFields(t, mustTeamPatchError(t, current, domain.TeamPatch{MemberIDs: &[]uuid.UUID{}})); fields["member_ids"] == "" {
		t.Fatal("empty members accepted")
	}
}

func mustTeamPatchError(t *testing.T, current domain.TeamInput, patch domain.TeamPatch) error {
	t.Helper()
	_, err := ApplyTeamPatch(current, patch)
	if err == nil {
		t.Fatal("patch accepted")
	}
	return err
}
