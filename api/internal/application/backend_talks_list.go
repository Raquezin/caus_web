package application

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (b *Backend) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	if err := ValidatePage(page); err != nil {
		return domain.TalkPage{}, err
	}
	if err := ValidateTalkFilter(filter); err != nil {
		return domain.TalkPage{}, err
	}
	fetch := page
	fetch.Limit = page.Limit + 1
	result, err := b.repo.ListTalks(ctx, fetch, filter)
	if err != nil {
		return domain.TalkPage{}, err
	}
	hasMore := len(result.Talks) > page.Limit
	if hasMore {
		result.Talks = result.Talks[:page.Limit]
	}
	return domain.TalkPage{Talks: result.Talks, HasMore: hasMore}, nil
}
