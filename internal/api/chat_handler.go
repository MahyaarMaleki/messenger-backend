package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	var otherParticipant *userProfileResponse // Only set for private chats

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

		profile := newUserProfileResponse(targetUser)
		otherParticipant = &profile

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
			_ = encoder.Encode(newConversationResponse(finalConversation, otherParticipant))
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
	_ = encoder.Encode(newConversationResponse(finalConversation, otherParticipant))
}

// getUserConversations returns the list of chats for the logged-in user
func (server *Server) getUserConversations(w http.ResponseWriter, r *http.Request) {
	encoder := json.NewEncoder(w)
	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// Fetch from DB
	rows, err := server.store.GetUserConversations(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := make([]conversationResponse, len(rows))
	for i, row := range rows {
		// Base Conversation Data
		c := conversationResponse{
			ID:            row.ID,
			Name:          row.Name.String, // "Dev Team" or ""
			Type:          row.Type,
			LastMessageAt: row.LastMessageAt.Time,
			CreatedAt:     row.CreatedAt.Time,
		}

		// Logic: If it's a private chat, populate 'OtherParticipant'
		// We check if OtherUsername is valid (not null)
		if row.Type == "private" && row.OtherUsername.Valid {
			c.OtherParticipant = &userProfileResponse{
				Username:  row.OtherUsername.String,
				FirstName: row.OtherFirstName.String,
				LastName:  row.OtherLastName.String,
				Bio:       row.OtherBio.String,
				AvatarUrl: row.OtherAvatarUrl.String,
			}
		}

		res[i] = c
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

func (server *Server) updateMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Parse IDs
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid message ID"))
		return
	}

	// 2. Parse Body
	req := new(updateMessageRequest)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse(InvalidJsonMsg))
		return
	}

	// 3. Get Existing Message
	message, err := server.store.GetMessage(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(errorResponse("Message not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 4. Security Checks
	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// Check A: Does this message belong to this chat?
	if message.ConversationID != conversationID {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Message does not belong to this conversation"))
		return
	}

	// Check B: Are you the sender?
	if message.SenderID != authPayload.UserID {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("You can only edit your own messages"))
		return
	}

	// 5. Update
	updatedMessage, err := server.store.UpdateMessage(r.Context(), db.UpdateMessageParams{
		ID:      messageID,
		Content: req.Content,
	})

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// For simplicity, we return the message without attachments here
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(newMessageResponse(updatedMessage, nil))
}

func (server *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	// 1. Parse IDs
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid message ID"))
		return
	}

	// 2. Get Existing Message
	message, err := server.store.GetMessage(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(errorResponse("Message not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 3. Security Checks
	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	if message.ConversationID != conversationID {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Message does not belong to this conversation"))
		return
	}

	if message.SenderID != authPayload.UserID {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("You can only delete your own messages"))
		return
	}

	// 4. Delete
	err = server.store.DeleteMessage(r.Context(), messageID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Message deleted successfully"})
}

func (server *Server) addParticipant(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	req := new(addParticipantRequest)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse(InvalidJsonMsg))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// A. Check My Role
	myself, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil || myself.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("Only admins can add members"))
		return
	}

	// B. Fetch Conversation to check Type (Group vs Channel)
	conversation, err := server.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	//  C. Determine Role based on Type
	targetRole := "member" // Default for groups
	if conversation.Type == "channel" {
		targetRole = "observer" // Read-only for channels
	}

	// D. Find Target User
	user, err := server.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResponse("User not found"))
		return
	}

	// E. Add them with the correct role
	_, err = server.store.AddParticipant(r.Context(), db.AddParticipantParams{
		ConversationID: conversationID,
		UserID:         user.ID,
		Role:           targetRole,
	})
	if err != nil {
		if errorCode(err) == UniqueViolation {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(errorResponse("User already in group"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": fmt.Sprintf("User added as %s", targetRole),
	})
}

// joinChannel allows a user to subscribe to a public channel
func (server *Server) joinChannel(w http.ResponseWriter, r *http.Request) {
	// 1. Parse Conversation ID
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 2. Fetch Conversation to check Type
	conversation, err := server.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(errorResponse("Channel not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 3. Strict Check: Only Channels allow public joining
	// (We block users from joining private Groups or 1-on-1 chats this way)
	if conversation.Type != "channel" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("This conversation is invite-only"))
		return
	}

	// 4. Add Self as Observer
	_, err = server.store.AddParticipant(r.Context(), db.AddParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
		Role:           "observer", // Channels are read-only for subscribers
	})

	if err != nil {
		// Handle "Already Joined" case gracefully
		if errorCode(err) == UniqueViolation {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(errorResponse("You are already subscribed to this channel"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Subscribed successfully"})
}

func (server *Server) leaveConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// Execute Delete (If user isn't in chat, this is a no-op, which is fine)
	err = server.store.RemoveParticipant(r.Context(), db.RemoveParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Left conversation successfully"})
}

// 2. Kick Participant (Admin Only)
func (server *Server) removeParticipant(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	targetUserID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid user ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// A. Check My Role
	myself, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil || myself.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("Only admins can remove members"))
		return
	}

	// B. Remove Target
	err = server.store.RemoveParticipant(r.Context(), db.RemoveParticipantParams{
		ConversationID: conversationID,
		UserID:         targetUserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "User removed"})
}

func (server *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	req := new(updateConversationRequest)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse(InvalidJsonMsg))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// A. Check My Role
	myself, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil || myself.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(errorResponse("Only admins can update group or channel info"))
		return
	}

	// B. Update
	updatedChat, err := server.store.UpdateConversation(r.Context(), db.UpdateConversationParams{
		ID:   conversationID,
		Name: pgtype.Text{String: req.Name, Valid: true},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(newConversationResponse(updatedChat, nil))
}
