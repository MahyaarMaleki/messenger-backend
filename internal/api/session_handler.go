package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

func (server *Server) createSession(w http.ResponseWriter, r *http.Request) {
	req := new(createSessionRequest)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)

	if err := decoder.Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(InvalidJsonMsg))
		return
	}

	user, err := server.store.GetUserByUsername(r.Context(), req.Username)
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

	if !util.ComparePassword(req.Password, user.PasswordHash) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Invalid credentials"))
		return
	}

	// Create Access Token
	accessToken, accessPayload, err := server.tokenMaker.Create(
		user.ID,
		server.config.AccessTokenDuration,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// Create Refresh Token
	refreshToken, refreshPayload, err := server.tokenMaker.Create(
		user.ID,
		server.config.RefreshTokenDuration,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// Save Session to DB
	session, err := server.store.CreateSession(r.Context(), db.CreateSessionParams{
		ID:           refreshPayload.ID, // Use the ID from the Paseto Token
		UserID:       user.ID,
		RefreshToken: refreshToken,
		UserAgent:    r.UserAgent(), // "Mozilla/5.0..."
		ClientIp:     r.RemoteAddr,  // IP address of the user
		IsBlocked:    false,
		ExpiresAt: pgtype.Timestamptz{
			Time:  refreshPayload.ExpiredAt,
			Valid: true,
		},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := createSessionResponse{
		SessionID:             session.ID,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessPayload.ExpiredAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshPayload.ExpiredAt,
		User:                  newUserResponse(user),
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}
