package httpapi

import "caus-platform/api/internal/domain"

type accountPatchRequest struct {
	FirstName jsonField `json:"first_name"`
	LastName  jsonField `json:"last_name"`
}

func (request accountPatchRequest) patch() (domain.AccountPatch, error) {
	fields := map[string]string{}
	patch := domain.AccountPatch{
		FirstName: patchedText(request.FirstName, "first_name", fields),
		LastName:  patchedText(request.LastName, "last_name", fields),
	}
	if err := invalidFields(fields); err != nil {
		return domain.AccountPatch{}, err
	}
	return patch, nil
}
