package application

import (
	"context"
	"unicode/utf8"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) UpdateAccount(ctx context.Context, id uuid.UUID, patch domain.AccountPatch) (domain.Account, error) {
	if patch.FirstName == nil && patch.LastName == nil {
		return domain.Account{}, emptyPatchError()
	}
	fields := map[string]string{}
	if patch.FirstName != nil {
		requireName(fields, "first_name", *patch.FirstName)
	}
	if patch.LastName != nil {
		lastName := *patch.LastName
		if !domain.ValidText(lastName) {
			fields["last_name"] = "last_name contains invalid text"
		} else if utf8.RuneCountInString(lastName) > maxNameLength {
			fields["last_name"] = "last_name must be at most 200 characters"
		}
	}
	if err := domain.NewValidationError(fields); err != nil {
		return domain.Account{}, err
	}
	return b.repo.UpdateAccount(ctx, id, patch)
}
