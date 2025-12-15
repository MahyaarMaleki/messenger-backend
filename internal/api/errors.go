package api

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ForeignKeyViolation = "23503"
	UniqueViolation     = "23505"
)

// errorCode extracts the SQL State code
func errorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}

	return ""
}

// errorResponse wraps the error message in a JSON object
func errorResponse(err error) map[string]any {
	return map[string]any{
		"error": err.Error(),
	}
}
