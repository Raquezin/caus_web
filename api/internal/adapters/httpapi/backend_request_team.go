package httpapi

import (
	"caus-platform/api/internal/domain"
)

type teamRequest struct {
	Name      jsonField `json:"name"`
	PhotoURL  jsonField `json:"photo_url"`
	Position  jsonField `json:"position"`
	MemberIDs jsonField `json:"member_ids"`
}

func (request teamRequest) input() (domain.TeamInput, error) {
	fields := map[string]string{}
	input := domain.TeamInput{
		Name:      nullableText(request.Name, "name", fields),
		PhotoURL:  nullableText(request.PhotoURL, "photo_url", fields),
		Position:  nullableNumber[int](request.Position, "position", fields),
		MemberIDs: requiredUUIDs(request.MemberIDs, "member_ids", fields),
	}
	if err := invalidFields(fields); err != nil {
		return domain.TeamInput{}, err
	}
	return input, nil
}

type teamPatchRequest struct {
	Name      jsonField `json:"name"`
	PhotoURL  jsonField `json:"photo_url"`
	Position  jsonField `json:"position"`
	MemberIDs jsonField `json:"member_ids"`
}

func (request teamPatchRequest) patch() (domain.TeamPatch, error) {
	fields := map[string]string{}
	patch := domain.TeamPatch{
		Name:      patchedNullableText(request.Name, "name", fields),
		PhotoURL:  patchedNullableText(request.PhotoURL, "photo_url", fields),
		Position:  patchedNullableInt(request.Position, "position", fields),
		MemberIDs: patchedUUIDs(request.MemberIDs, "member_ids", fields),
	}
	if err := invalidFields(fields); err != nil {
		return domain.TeamPatch{}, err
	}
	return patch, nil
}
