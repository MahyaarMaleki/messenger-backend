package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
)

type validationResponse struct {
	Error   string            `json:"error"`
	Details map[string]string `json:"details"`
}

// validateRequest checks the struct tags and writes a 400 response if validation fails.
// Returns true if validation passed, false if it failed.
func (server *Server) validateRequest(w http.ResponseWriter, req interface{}, encoder *json.Encoder) bool {
	if err := server.validator.Struct(req); err != nil {
		// Check if it's a validation error
		var valErrors validator.ValidationErrors
		if errors.As(err, &valErrors) {
			// Use the helper to generate the standard response
			res := validationErrorResponse(err)
			w.WriteHeader(http.StatusBadRequest)
			_ = encoder.Encode(res)
			return false
		}

		// Fallback for other types of errors (e.g., bad struct tags)
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(err.Error()))
		return false
	}

	return true
}

// validationErrorResponse formats the validation errors into a clean structure
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
	case "url":
		return "Invalid URL format"
	default:
		return fe.Error()
	}
}
