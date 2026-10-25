package application

import (
	"context"
	"strings"

	"caus-platform/api/internal/domain"
)

func (b *Backend) ListUsers(ctx context.Context, query string, page domain.Page) (domain.UserPage, error) {
	search := strings.TrimSpace(query)
	if !domain.ValidText(search) || len(search) > domain.MaxUserSearchLength {
		return domain.UserPage{}, domain.ValidationError{Fields: map[string]string{
			"q": "q must be valid text and at most 200 bytes",
		}}
	}
	if err := ValidatePage(page); err != nil {
		return domain.UserPage{}, err
	}
	result, err := b.repo.ListUsers(ctx, search, domain.Page{Limit: page.Limit + 1, Offset: page.Offset})
	if err != nil {
		return domain.UserPage{}, err
	}
	result.HasMore = len(result.Users) > page.Limit
	if result.HasMore {
		result.Users = result.Users[:page.Limit]
	}
	return result, nil
}
