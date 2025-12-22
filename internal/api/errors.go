package api

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ForeignKeyViolation = "23503"
	UniqueViolation     = "23505"
)

type validationResponse struct {
	Error   string            `json:"error"`
	Details map[string]string `json:"details"`
}

// errorCode extracts the SQL State code
func errorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}

	return ""
}

func errorResponse(err error) map[string]any {
	return map[string]any{
		"error": err.Error(),
	}
}

func validationErrorResponse(err error) validationResponse {
	details := make(map[string]string)

	var valErrors validator.ValidationErrors
	if errors.As(err, &valErrors) {
		for _, fe := range valErrors {
			details[fe.Field()] = msgForTag(fe)
		}
	}

	return validationResponse{
		Error:   "Invalid input parameters",
		Details: details,
	}
}

// msgForTag customizes the error message for each validation tag
func msgForTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "email":
		return "Invalid email format"
	case "min":
		return fmt.Sprintf("Must be at least %s characters long", fe.Param())
	case "max":
		return fmt.Sprintf("Must be at most %s characters long", fe.Param())
	default:
		return fe.Field()
	}
}
