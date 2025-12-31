package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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

	if !server.validateRequest(w, req, encoder) {
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

func (server *Server) renewAccessToken(w http.ResponseWriter, r *http.Request) {
	req := new(renewAccessTokenRequest)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)

	if err := decoder.Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(InvalidJsonMsg))
		return
	}

	if !server.validateRequest(w, req, encoder) {
		return
	}

	// Verify Refresh Token (Crypto check)
	refreshPayload, err := server.tokenMaker.Verify(req.RefreshToken)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Invalid or expired refresh token"))
		return
	}

	// Find Session in DB
	session, err := server.store.GetSession(r.Context(), refreshPayload.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("Session not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// -- Security Checks --

	// Check if session is blocked
	if session.IsBlocked {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Blocked session"))
		return
	}

	// Check if user matches
	if session.UserID != refreshPayload.UserID {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Incorrect session user"))
		return
	}

	// Check if token matches
	if session.RefreshToken != req.RefreshToken {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Mismatched session token"))
		return
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt.Time) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = encoder.Encode(errorResponse("Expired session"))
		return
	}

	// Generate New Access Token
	accessToken, accessPayload, err := server.tokenMaker.Create(
		refreshPayload.UserID,
		server.config.AccessTokenDuration,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := renewAccessTokenResponse{
		AccessToken:          accessToken,
		AccessTokenExpiresAt: accessPayload.ExpiredAt,
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}

func (server *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	req := new(revokeSessionRequest)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)

	if err := decoder.Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(InvalidJsonMsg))
		return
	}

	if !server.validateRequest(w, req, encoder) {
		return
	}

	// Block Session in DB
	if err := server.store.BlockSession(r.Context(), req.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("Session not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(map[string]string{"message": "Session revoked successfully"})
}
