package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
)

func (server *Server) generateVoiceToken(w http.ResponseWriter, r *http.Request) {
	conversationIDStr := chi.URLParam(r, "id")
	conversationID, err := uuid.Parse(conversationIDStr)
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// Check if they are actually a member of this chat
	_, err = server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusForbidden)
			_ = encoder.Encode(errorResponse("Access denied"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	user, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	canPublish := true
	canSubscribe := true

	// Use the Conversation uuid string as the "Room Name" so everyone in the group joins the same voice room
	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         conversationIDStr,
		CanPublish:   &canPublish,
		CanSubscribe: &canSubscribe,
	}

	accessToken := auth.NewAccessToken(server.config.LiveKitAPIKey, server.config.LiveKitAPISecret)
	accessToken.SetVideoGrant(grant).
		SetIdentity(user.ID.String()).
		SetName(user.Username).
		SetValidFor(2 * time.Hour)

	tokenString, err := accessToken.ToJWT()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse("Failed to generate voice token"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(map[string]string{
		"token": tokenString,
	})
}
