package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

// createUserRequest defines the expected JSON body for registration
type createUserRequest struct {
	Username  string `json:"username" validate:"required,min=3,max=30"`
	Password  string `json:"password" validate:"required"`
	Email     string `json:"email" validate:"required,email"`
	FirstName string `json:"firstName" validate:"required,max=50"`
	LastName  string `json:"lastName" validate:"required,max=50"`
}

// userResponse is the safe DTO that excludes sensitive fields like PasswordHash
type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	CreatedAt time.Time `json:"createdAt"`
}

// newUserResponse converts a database user model to an API response
func newUserResponse(user db.User) userResponse {
	return userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		CreatedAt: user.CreatedAt.Time, // Extracts time.Time from pgtype.Timestamptz
	}
}

// createUser handles new user registration
func (server *Server) createUser(w http.ResponseWriter, r *http.Request) {
	req := new(createUserRequest)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)

	// Decode JSON
	if err := decoder.Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(err))
		return
	}

	// Validate content
	if err := server.validator.Struct(req); err != nil {
		var valErrors validator.ValidationErrors
		if errors.As(err, &valErrors) {
			out := make(map[string]string)
			for _, fe := range valErrors {
				out[fe.Field()] = msgForTag(fe)
			}
		}

		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(validationErrorResponse(err))
		return
	}

	// Hash password
	hashedPassword, err := util.HashPassword(req.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(err))
		return
	}

	// Create user in database
	arg := db.CreateUserParams{
		Username:     req.Username,
		PasswordHash: hashedPassword,
		Email:        req.Email,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
	}

	user, err := server.store.CreateUser(r.Context(), arg)
	if err != nil {
		// Handle "Unique Violation"
		if errorCode(err) == UniqueViolation {
			w.WriteHeader(http.StatusForbidden)
			_ = encoder.Encode(errorResponse(errors.New("username or email already exists")))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(err))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(newUserResponse(user))
}

// getUser handles fetching a user by their username
func (server *Server) getUser(w http.ResponseWriter, r *http.Request) {
	// We will implement this next:
	// username := chi.URLParam(r, "username")
	w.Write([]byte("Not implemented yet"))
}
