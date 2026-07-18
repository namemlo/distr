package db

import (
	"errors"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

func isProtectedReferenceViolation(err error) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return false
	}
	return pgError.Code == pgerrcode.ForeignKeyViolation || pgError.Code == pgerrcode.RestrictViolation
}
