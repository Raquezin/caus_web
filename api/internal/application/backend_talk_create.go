package application

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (b *Backend) CreateTalk(ctx context.Context, input domain.TalkInput) (domain.Talk, error) {
	if err := ValidateTalkInput(input); err != nil {
		return domain.Talk{}, err
	}
	return b.repo.CreateTalk(ctx, input)
}
