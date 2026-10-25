package postgres

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ BackendDB = (*pgxpool.Pool)(nil)
var _ BackendDB = pgx.Tx(nil)
