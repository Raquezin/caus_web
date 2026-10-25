package application

import (
	"caus-platform/api/internal/domain"
)

func ApplyTalkPatch(current domain.TalkInput, patch domain.TalkPatch) (domain.TalkInput, error) {
	if patch.Title == nil && patch.Description == nil && patch.Location == nil && patch.Language == nil &&
		patch.Date == nil && patch.Duration == nil && patch.StreamURL == nil && patch.PosterURL == nil &&
		patch.Published == nil && patch.SpeakerIDs == nil {
		return domain.TalkInput{}, emptyPatchError()
	}
	merged := current
	if patch.Title != nil {
		merged.Title = *patch.Title
	}
	if patch.Description != nil {
		merged.Description = *patch.Description
	}
	if patch.Location != nil {
		merged.Location = *patch.Location
	}
	if patch.Language != nil {
		merged.Language = *patch.Language
	}
	if patch.Date != nil {
		merged.Date = *patch.Date
	}
	if patch.Duration != nil {
		merged.Duration = *patch.Duration
	}
	if patch.StreamURL != nil {
		merged.StreamURL = patch.StreamURL.Value
	}
	if patch.PosterURL != nil {
		merged.PosterURL = patch.PosterURL.Value
	}
	if patch.Published != nil {
		merged.Published = *patch.Published
	}
	if patch.SpeakerIDs != nil {
		merged.SpeakerIDs = *patch.SpeakerIDs
	}
	if err := ValidateTalkInput(merged); err != nil {
		return domain.TalkInput{}, err
	}
	return merged, nil
}

func ApplyCompetitionPatch(current domain.CompetitionInput, patch domain.CompetitionPatch) (domain.CompetitionInput, error) {
	if patch.Name == nil && patch.Description == nil && patch.Location == nil && patch.Date == nil && patch.TeamSize == nil {
		return domain.CompetitionInput{}, emptyPatchError()
	}
	merged := current
	if patch.Name != nil {
		merged.Name = *patch.Name
	}
	if patch.Description != nil {
		merged.Description = *patch.Description
	}
	if patch.Location != nil {
		merged.Location = *patch.Location
	}
	if patch.Date != nil {
		merged.Date = *patch.Date
	}
	if patch.TeamSize != nil {
		merged.TeamSize = *patch.TeamSize
	}
	if err := ValidateCompetitionInput(merged); err != nil {
		return domain.CompetitionInput{}, err
	}
	return merged, nil
}

func ApplyTeamPatch(current domain.TeamInput, patch domain.TeamPatch) (domain.TeamInput, error) {
	if patch.Name == nil && patch.PhotoURL == nil && patch.Position == nil && patch.MemberIDs == nil {
		return domain.TeamInput{}, emptyPatchError()
	}
	merged := current
	if patch.Name != nil {
		merged.Name = patch.Name.Value
	}
	if patch.PhotoURL != nil {
		merged.PhotoURL = patch.PhotoURL.Value
	}
	if patch.Position != nil {
		merged.Position = patch.Position.Value
	}
	if patch.MemberIDs != nil {
		merged.MemberIDs = *patch.MemberIDs
	}
	if err := ValidateTeamInput(merged); err != nil {
		return domain.TeamInput{}, err
	}
	return merged, nil
}

func emptyPatchError() error {
	return domain.ValidationError{Fields: map[string]string{"patch": "patch must change at least one field"}}
}
