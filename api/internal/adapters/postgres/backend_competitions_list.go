package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error) {
	if page.Limit < 1 || page.Offset < 0 {
		return domain.CompetitionPage{}, domain.ErrInvalidPagination
	}
	rows, err := r.db.Query(ctx,
		"SELECT "+competitionColumns+" FROM competitions ORDER BY date DESC, id ASC LIMIT $1 OFFSET $2",
		page.Limit, page.Offset)
	if err != nil {
		return domain.CompetitionPage{}, translateBackendError(err)
	}
	defer rows.Close()
	competitions := make([]domain.Competition, 0, page.Limit)
	for rows.Next() {
		var competition domain.Competition
		var id pgtype.UUID
		var date time.Time
		if err := rows.Scan(&id, &competition.Name, &competition.Description, &date, &competition.Location,
			&competition.TeamSize, &competition.CreatedAt, &competition.UpdatedAt); err != nil {
			return domain.CompetitionPage{}, translateBackendError(err)
		}
		competition.ID = id.Bytes
		competition.Date = naiveTimestamp(date)
		competitions = append(competitions, competition)
	}
	if err := rows.Err(); err != nil {
		return domain.CompetitionPage{}, translateBackendError(err)
	}
	return domain.CompetitionPage{Competitions: competitions}, nil
}
