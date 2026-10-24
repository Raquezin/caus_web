package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type accountResponse struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *Auth) me(w http.ResponseWriter, r *http.Request) {
	cookies := r.CookiesNamed("caus_session")
	if len(cookies) != 1 || cookies[0].Value == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	account, err := a.service.CurrentUser(ctx, cookies[0].Value)
	if errors.Is(err, domain.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}
	if err != nil {
		a.log.Error("current user failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, accountResponse{
		ID:        account.ID,
		FirstName: account.FirstName,
		LastName:  account.LastName,
		Email:     account.Email,
		Role:      account.Role,
		CreatedAt: account.CreatedAt.UTC(),
		UpdatedAt: account.UpdatedAt.UTC(),
	})
}
