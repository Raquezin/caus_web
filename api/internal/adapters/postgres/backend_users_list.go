package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

const listUsersQuery = "SELECT " + userColumns + " FROM people WHERE (" +
	"$1 = ''" +
	" OR strpos(lower(first_name), lower($1)) > 0" +
	" OR strpos(lower(last_name), lower($1)) > 0" +
	" OR strpos(lower(first_name || ' ' || last_name), lower($1)) > 0" +
	" OR strpos(lower(coalesce(email, '')), lower($1)) > 0" +
	") ORDER BY id LIMIT $2 OFFSET $3"

func (r *BackendRepository) ListUsers(ctx context.Context, query string, page domain.Page) (domain.UserPage, error) {
	rows, err := r.db.Query(ctx, listUsersQuery, query, page.Limit, page.Offset)
	if err != nil {
		return domain.UserPage{}, translateBackendError(err)
	}
	defer rows.Close()
	users := make([]domain.User, 0)
	for rows.Next() {
		var user domain.User
		var id pgtype.UUID
		if err := rows.Scan(&id, &user.FirstName, &user.LastName, &user.Email, &user.Role,
			&user.CreatedAt, &user.UpdatedAt); err != nil {
			return domain.UserPage{}, translateBackendError(err)
		}
		user.ID = id.Bytes
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return domain.UserPage{}, translateBackendError(err)
	}
	return domain.UserPage{Users: users, HasMore: false}, nil
}
