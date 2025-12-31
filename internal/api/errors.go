package api

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ForeignKeyViolation    = "23503"
	UniqueViolation        = "23505"
	InternalServerErrorMsg = "Something went wrong"
	InvalidJsonMsg         = "Invalid JSON format"
)

// errorCode extracts the SQL State code
func errorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}

	return ""
}

func errorResponse(errMsg string) map[string]string {
	return map[string]string{
		"error": errMsg,
	}
}
