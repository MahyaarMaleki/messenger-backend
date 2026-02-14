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

func (server *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	req := new(createConversationRequest)
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

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)
	var finalConversation db.Conversation

	// Case 1: Private Chat (1-on-1)
	if req.Type == "private" {
		targetUser, err := server.store.GetUserByUsername(r.Context(), *req.TargetUsername)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				w.WriteHeader(http.StatusNotFound)
				_ = encoder.Encode(errorResponse("Target user not found"))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
			return
		}

		// Check if chat already exists
		existingID, err := server.store.FindExistingPrivateChat(r.Context(), db.FindExistingPrivateChatParams{
			UserID:   authPayload.UserID,
			UserID_2: targetUser.ID,
		})

		// If found, return it immediately (don't create duplicate)
		if err == nil {
			finalConversation, err = server.store.GetConversation(r.Context(), existingID)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = encoder.Encode(newConversationResponse(finalConversation))
			return
		}

		// Create NEW Private Chat in Transaction
		err = server.store.ExecTx(r.Context(), func(q *db.Queries) error {
			// 1. Create Conversation
			var err error
			finalConversation, err = q.CreateConversation(r.Context(), db.CreateConversationParams{
				Name: pgtype.Text{Valid: false}, // No name for private chats
				Type: "private",
			})
			if err != nil {
				return err
			}

			// 2. Add Sender
			_, err = q.AddParticipant(r.Context(), db.AddParticipantParams{
				ConversationID: finalConversation.ID,
				UserID:         authPayload.UserID,
				Role:           "member",
			})
			if err != nil {
				return err
			}

			// 3. Add Target
			_, err = q.AddParticipant(r.Context(), db.AddParticipantParams{
				ConversationID: finalConversation.ID,
				UserID:         targetUser.ID,
				Role:           "member",
			})
			return err
		})

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
			return
		}
	} else {
		// Case 2: Group or Channel
		err := server.store.ExecTx(r.Context(), func(q *db.Queries) error {
			// 1. Create Conversation
			var err error
			finalConversation, err = q.CreateConversation(r.Context(), db.CreateConversationParams{
				Name: pgtype.Text{String: *req.Name, Valid: true},
				Type: req.Type,
			})
			if err != nil {
				return err
			}

			// 2. Add Creator as Admin
			_, err = q.AddParticipant(r.Context(), db.AddParticipantParams{
				ConversationID: finalConversation.ID,
				UserID:         authPayload.UserID,
				Role:           "admin",
			})
			return err
		})

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(newConversationResponse(finalConversation))
}
