package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

// getUserConversations returns the list of chats for the logged-in user
func (server *Server) getUserConversations(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// Fetch from DB (already sorted by last_message_at DESC in the query)
	conversations, err := server.store.GetUserConversations(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// Map to Response
	// Note: The query returns a custom Row struct, not db.Conversation,
	// so we map it manually or create a helper if reused often.
	res := make([]conversationResponse, len(conversations))
	for i, c := range conversations {
		res[i] = conversationResponse{
			ID:            c.ID,
			Name:          c.Name.String,
			Type:          c.Type,
			LastMessageAt: c.LastMessageAt.Time,
			CreatedAt:     c.CreatedAt.Time,
			// You could also return c.Role or c.JoinedAt if you update the DTO
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}

// createMessage posts a text message to a specific conversation
func (server *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Get ConversationID from URL
	conversationIDStr := chi.URLParam(r, "id")
	conversationID, err := uuid.Parse(conversationIDStr)
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	// 2. Parse Body
	req := new(createMessageRequest)
	// We decode into the struct which contains Content AND Attachments
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse(InvalidJsonMsg))
		return
	}

	if !server.validateRequest(w, req, encoder) {
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 3. Security Check: Is the user a participant?
	participant, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
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

	// Check for read-only roles
	if participant.Role == "observer" {
		w.WriteHeader(http.StatusForbidden)
		_ = encoder.Encode(errorResponse("Read-only access"))
		return
	}

	var message db.Message

	// 4. Transaction: Insert Message + Attachments + Bump Timestamp
	err = server.store.ExecTx(r.Context(), func(q *db.Queries) error {
		var err error
		// A. Create Message
		message, err = q.CreateMessage(r.Context(), db.CreateMessageParams{
			ConversationID: conversationID,
			SenderID:       authPayload.UserID,
			Content:        req.Content,
		})
		if err != nil {
			return err
		}

		// B. Create Attachments
		// We iterate over the attachments provided in the request
		for _, att := range req.Attachments {
			err = q.CreateAttachment(r.Context(), db.CreateAttachmentParams{
				MessageID: message.ID,
				FileUrl:   att.URL,
				FileType:  att.Type,
				FileName:  att.Name,
			})
			if err != nil {
				return err
			}
		}

		// C. Bump Conversation LastMessageAt
		return q.UpdateConversationLastMessageAt(r.Context(), db.UpdateConversationLastMessageAtParams{
			ID:            conversationID,
			LastMessageAt: pgtype.Timestamptz{Time: message.CreatedAt.Time, Valid: true},
		})
	})

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = encoder.Encode(newMessageResponse(message, req.Attachments))
}

// getMessages loads history with pagination
func (server *Server) getMessages(w http.ResponseWriter, r *http.Request) {
	conversationIDStr := chi.URLParam(r, "id")
	conversationID, err := uuid.Parse(conversationIDStr)
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	// Pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20 // Default 20 messages per page
	}
	offset := (page - 1) * limit

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 1. Security Check: Participant only
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

	// 2. Fetch Messages from DB
	// 'rows' is now a slice of a custom generated struct (e.g. GetConversationMessagesRow)
	// containing the standard fields PLUS the 'Attachments' []byte field.
	rows, err := server.store.GetConversationMessages(r.Context(), db.GetConversationMessagesParams{
		ConversationID: conversationID,
		Limit:          int32(limit),
		Offset:         int32(offset),
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := make([]messageResponse, len(rows))
	for i, row := range rows {
		// A. Parse the JSONB Attachments
		var attachments []attachmentDTO
		// row.Attachments is []byte (from the ::jsonb column)
		if err := json.Unmarshal(row.Attachments, &attachments); err != nil {
			// If JSON is corrupted or null, default to empty to prevent crash
			attachments = []attachmentDTO{}
		}

		// B. Reconstruct the db.Message object manually
		// We do this because 'row' is a special sqlc-generated struct, not the standard db.Message
		msg := db.Message{
			ID:             row.ID,
			ConversationID: row.ConversationID,
			SenderID:       row.SenderID,
			Content:        row.Content,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		}

		// C. Create Response
		res[i] = newMessageResponse(msg, attachments)
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}
