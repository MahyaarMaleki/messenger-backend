package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

// createUser handles new user registration
func (server *Server) createUser(w http.ResponseWriter, r *http.Request) {
	req := new(createUserRequest)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)

	// Decode JSON
	if err := decoder.Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(InvalidJsonMsg))
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
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
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
			_ = encoder.Encode(errorResponse("Username or email already exists"))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(newUserResponse(user))
}

// listUsers is used for search/filter
func (server *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	encoder := json.NewEncoder(w)

	// Defaults if params are missing/invalid
	if page < 1 {
		page = 1
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 100 {
		limit = 100
	}

	arg := db.ListUsersParams{
		Column1: search,
		Limit:   int32(limit),
		Offset:  int32((page - 1) * limit),
	}

	users, err := server.store.ListUsers(r.Context(), arg)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := make([]userProfileResponse, len(users))
	for i, user := range users {
		res[i] = newUserProfileResponse(user)
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}

// getUser handles fetching a user by their username
func (server *Server) getUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	encoder := json.NewEncoder(w)

	user, err := server.store.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("User not found"))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(newUserProfileResponse(user))
}
