package httpapi

import (
	"context"
	"net/http"
)

type userRoleRequest struct {
	Role jsonField `json:"role"`
}

func (b *Backend) updateUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	var request userRoleRequest
	if !decodeBody(w, r, &request) {
		return
	}
	fields := map[string]string{}
	role := requiredText(request.Role, "role", fields)
	if err := invalidFields(fields); err != nil {
		b.backendError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	user, err := b.service.UpdateUserRole(ctx, id, role)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userResponse(user))
}
