package httpapi

import (
	"caus-platform/api/internal/domain"
)

type competitionRequest struct {
	Name        jsonField `json:"name"`
	Description jsonField `json:"description"`
	Date        jsonField `json:"date"`
	Location    jsonField `json:"location"`
	TeamSize    jsonField `json:"team_size"`
}

func (request competitionRequest) input() (domain.CompetitionInput, error) {
	fields := map[string]string{}
	input := domain.CompetitionInput{
		Name:        requiredText(request.Name, "name", fields),
		Description: requiredText(request.Description, "description", fields),
		Date:        requiredDate(request.Date, "date", fields),
		Location:    requiredText(request.Location, "location", fields),
		TeamSize:    3,
	}
	if value, ok := strictNumber[int](request.TeamSize, "team_size", fields); ok {
		input.TeamSize = value
	}
	if err := invalidFields(fields); err != nil {
		return domain.CompetitionInput{}, err
	}
	return input, nil
}

type competitionPatchRequest struct {
	Name        jsonField `json:"name"`
	Description jsonField `json:"description"`
	Date        jsonField `json:"date"`
	Location    jsonField `json:"location"`
	TeamSize    jsonField `json:"team_size"`
}

func (request competitionPatchRequest) patch() (domain.CompetitionPatch, error) {
	fields := map[string]string{}
	patch := domain.CompetitionPatch{
		Name:        patchedText(request.Name, "name", fields),
		Description: patchedText(request.Description, "description", fields),
		Date:        patchedDate(request.Date, "date", fields),
		Location:    patchedText(request.Location, "location", fields),
		TeamSize:    patchedNumber[int](request.TeamSize, "team_size", fields),
	}
	if err := invalidFields(fields); err != nil {
		return domain.CompetitionPatch{}, err
	}
	return patch, nil
}
