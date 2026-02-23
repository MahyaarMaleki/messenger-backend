package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
	"github.com/mahyaarmaleki/messenger-backend/internal/util"
	"github.com/sashabaranov/go-openai"
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
		c := conversationResponse{
			ID:            row.ID,
			Name:          row.Name.String,
			Type:          row.Type,
			LastMessage:   row.LastMessage,
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

	senderUser, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	senderProfile := newUserProfileResponse(senderUser)

	// --- Real-Time Broadcast Logic ---

	// 1. Prepare the Response Payload
	// We construct the data once, then send this exact JSON to everyone
	response := newMessageResponse(message, &senderProfile, req.Attachments)

	// 2. Run Broadcast in Background
	// Use a goroutine so that the API responds immediately to the sender
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Fetch all participants of this conversation
		userIDs, err := server.store.GetConversationParticipants(ctx, conversationID)
		if err != nil {
			log.Printf("CRITICAL: Failed to broadcast message %s: %v", message.ID, err)
			return
		}

		// Send to Hub
		server.hub.Broadcast(userIDs, response)
	}()

	// 3. Respond to Client (Success)
	// The client gets this 201 regardless of whether the broadcast worked or failed.
	w.WriteHeader(http.StatusCreated)
	_ = encoder.Encode(response)
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

	conv, err := server.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("Conversation not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)
	userRole := "observer"

	// Fetch the actual role
	participantInfo, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err == nil {
		userRole = participantInfo.Role
	}

	// 1. Security Check: Participant only (except public channels)
	if conv.Type != "channel" && err != nil {
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
	}

	// 2. Fetch Messages from DB
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

	var res []messageResponse
	for _, row := range rows {
		var attachments []attachmentDTO
		if err := json.Unmarshal(row.Attachments, &attachments); err != nil {
			attachments = []attachmentDTO{}
		}

		msg := db.Message{
			ID:             row.ID,
			ConversationID: row.ConversationID,
			SenderID:       row.SenderID,
			Content:        row.Content,
			CreatedAt:      row.CreatedAt,
			UpdatedAt:      row.UpdatedAt,
		}

		senderProfile := &userProfileResponse{
			Username:  row.SenderUsername,
			FirstName: row.SenderFirstName,
			LastName:  row.SenderLastName,
			AvatarUrl: row.SenderAvatarUrl.String,
		}

		// Hide channel system messages from observers
		if conv.Type == "channel" && strings.HasPrefix(msg.Content, "SYSTEM_EVENT:") {
			if userRole != "admin" && userRole != "creator" {
				continue // Skip adding this message to the response
			}
		}

		res = append(res, newMessageResponse(msg, senderProfile, attachments))
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

	// 5. Update message
	updatedMessage, err := server.store.UpdateMessage(r.Context(), db.UpdateMessageParams{
		ID:      messageID,
		Content: req.Content,
	})

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 6. Fetch Sender Profile (Current User)
	senderUser, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	senderProfile := newUserProfileResponse(senderUser)

	// For simplicity, we return the message without attachments here
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(newMessageResponse(updatedMessage, &senderProfile, nil))
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

func (server *Server) getConversationParticipants(w http.ResponseWriter, r *http.Request) {
	conversationIDStr := chi.URLParam(r, "id")
	conversationID, err := uuid.Parse(conversationIDStr)
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	conversation, err := server.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("Conversation not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// Are you a member of this chat?
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

	// If it's a channel, only admins/creators can see members
	if conversation.Type == "channel" {
		if participant.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			_ = encoder.Encode(errorResponse("Only admins can view channel members"))
			return
		}
	}

	rows, err := server.store.GetConversationParticipantsDetailed(r.Context(), conversationID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := make([]participantResponse, len(rows))
	for i, row := range rows {
		res[i] = participantResponse{
			User: participantUser{
				ID:        row.ID,
				Username:  row.Username,
				FirstName: row.FirstName,
				LastName:  row.LastName,
				AvatarUrl: row.AvatarUrl.String,
			},
			Role:     row.Role,
			JoinedAt: row.JoinedAt.Time,
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
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

	// Broadcast the join message to admins
	user, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err == nil {
		systemMsgContent := user.Username + " has joined the channel."
		sysMessage, err := server.store.CreateMessage(r.Context(), db.CreateMessageParams{
			ConversationID: conversationID,
			SenderID:       authPayload.UserID,
			Content:        "SYSTEM_EVENT:" + systemMsgContent,
		})

		if err == nil {
			wsProfile := &userProfileResponse{
				Username:  user.Username,
				FirstName: user.FirstName,
				LastName:  user.LastName,
				AvatarUrl: user.AvatarUrl.String,
			}
			wsResponse := newMessageResponse(sysMessage, wsProfile, nil)

			adminParticipants, _ := server.store.GetAdminParticipants(r.Context(), conversationID)
			go server.hub.Broadcast(adminParticipants, wsResponse)
		} else {
			log.Printf("Failed to create system join message: %v", err)
		}
	} else {
		log.Printf("Failed to fetch user profile for join message: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Subscribed successfully"})
}

func (server *Server) leaveConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 1. Fetch the conversation to check its type
	conv, err := server.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			encoder.Encode(errorResponse("Conversation not found"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// ==========================================
	// SCENARIO A: PRIVATE CHAT (Hard Delete)
	// ==========================================
	if conv.Type == "private" {
		err := server.store.DeleteConversation(r.Context(), conversationID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			encoder.Encode(errorResponse(InternalServerErrorMsg))
			return
		}

		w.WriteHeader(http.StatusOK)
		encoder.Encode(map[string]string{"message": "Chat permanently deleted"})
		return
	}

	// ==========================================
	// SCENARIO B: GROUP / CHANNEL (Leave & Notify)
	// ==========================================

	// 1. Fetch the leaving user's profile and current role
	user, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	role, err := server.store.GetParticipantRole(r.Context(), db.GetParticipantRoleParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})

	// 2. SUCCESSION LOGIC: If they are an admin/creator, check if we need to promote someone
	if role == "admin" || role == "creator" {
		remainingAdmins, _ := server.store.CountRemainingAdmins(r.Context(), db.CountRemainingAdminsParams{
			ConversationID: conversationID,
			UserID:         authPayload.UserID, // Exclude the person leaving
		})

		// If no admins are left, find the next oldest member to promote
		if remainingAdmins == 0 {
			nextAdminID, err := server.store.GetOldestRemainingParticipant(r.Context(), db.GetOldestRemainingParticipantParams{
				ConversationID: conversationID,
				UserID:         authPayload.UserID,
			})

			// err will be sql.ErrNoRows if they were the literal last person in the group
			if err == nil {
				// Promote the oldest remaining member to admin
				_ = server.store.UpdateParticipantRole(r.Context(), db.UpdateParticipantRoleParams{
					ConversationID: conversationID,
					UserID:         nextAdminID,
					Role:           "admin",
				})

				// Send a second system message announcing the promotion!
				promotedUser, _ := server.store.GetUserById(r.Context(), nextAdminID)
				promoText := promotedUser.Username + " is now an admin."
				promoMsg, _ := server.store.CreateMessage(r.Context(), db.CreateMessageParams{
					ConversationID: conversationID,
					SenderID:       authPayload.UserID,
					Content:        "SYSTEM_EVENT:" + promoText,
				})

				// Broadcast the promotion message
				participants, _ := server.store.GetConversationParticipants(r.Context(), conversationID)
				wsProfile := &userProfileResponse{
					Username:  user.Username,
					FirstName: user.FirstName,
					LastName:  user.LastName,
					AvatarUrl: user.AvatarUrl.String,
				}
				go server.hub.Broadcast(participants, newMessageResponse(promoMsg, wsProfile, nil))
			}
		}
	}

	// 3. Create the standard "Left Chat" System Message
	targetType := "chat"
	if conv.Type == "group" {
		targetType = "group"
	} else if conv.Type == "channel" {
		targetType = "channel"
	}

	systemMsgContent := user.Username + " has left the " + targetType + "."
	sysMessage, err := server.store.CreateMessage(r.Context(), db.CreateMessageParams{
		ConversationID: conversationID,
		SenderID:       authPayload.UserID,
		Content:        "SYSTEM_EVENT:" + systemMsgContent,
	})

	if err == nil {
		wsProfile := &userProfileResponse{
			Username:  user.Username,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			AvatarUrl: user.AvatarUrl.String,
		}
		wsResponse := newMessageResponse(sysMessage, wsProfile, nil)

		// 4. RESTRICTED BROADCAST LOGIC
		if conv.Type == "channel" {
			// For channels, we only want admins to get the live WebSocket event
			adminParticipants, _ := server.store.GetAdminParticipants(r.Context(), conversationID)
			go server.hub.Broadcast(adminParticipants, wsResponse)
		} else {
			// For groups and private chats, everyone gets it
			participants, _ := server.store.GetConversationParticipants(r.Context(), conversationID)
			go server.hub.Broadcast(participants, wsResponse)
		}
	}

	// 4. Execute Delete: Remove the leaving user from the participants table
	err = server.store.RemoveParticipant(r.Context(), db.RemoveParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 5. Cleanup edge case: If the group is now completely empty, delete it
	remainingMembers, _ := server.store.GetConversationParticipants(r.Context(), conversationID)
	if len(remainingMembers) == 0 {
		_ = server.store.DeleteConversation(r.Context(), conversationID)
	}

	w.WriteHeader(http.StatusOK)
	encoder.Encode(map[string]string{"message": "Left conversation successfully"})
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

	// B. Fetch user details for the system message
	targetUser, err := server.store.GetUserById(r.Context(), targetUserID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResponse("Target user not found"))
		return
	}

	adminUser, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse("Could not fetch admin profile"))
		return
	}

	// C. Remove Target
	err = server.store.RemoveParticipant(r.Context(), db.RemoveParticipantParams{
		ConversationID: conversationID,
		UserID:         targetUserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// D. Create System Message & Broadcast
	systemMsgContent := adminUser.Username + " removed " + targetUser.Username + " from the chat."
	sysMessage, err := server.store.CreateMessage(r.Context(), db.CreateMessageParams{
		ConversationID: conversationID,
		SenderID:       authPayload.UserID,
		Content:        "SYSTEM_EVENT:" + systemMsgContent,
	})

	if err == nil {
		wsProfile := &userProfileResponse{
			Username:  adminUser.Username,
			FirstName: adminUser.FirstName,
			LastName:  adminUser.LastName,
			AvatarUrl: adminUser.AvatarUrl.String,
		}
		wsResponse := newMessageResponse(sysMessage, wsProfile, nil)

		// Broadcast to the remaining participants
		participants, _ := server.store.GetConversationParticipants(r.Context(), conversationID)
		go server.hub.Broadcast(participants, wsResponse)
	} else {
		log.Printf("Failed to create kick system message: %v", err)
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

func (server *Server) MarkConversationAsRead(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// B. Update
	if err = server.store.UpdateParticipantLastRead(r.Context(), db.UpdateParticipantLastReadParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Last seen updated"})
}

// generateInvite creates a new invite link for a conversation (Admin only)
func (server *Server) generateInvite(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	encoder := json.NewEncoder(w)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	req := new(generateInviteRequest)
	// If body is empty, defaults will be 0. We'll set safe defaults below.
	_ = json.NewDecoder(r.Body).Decode(req)

	if req.MaxUses <= 0 {
		req.MaxUses = 1 // Default to one-time use
	}
	if req.ExpiresInHours <= 0 {
		req.ExpiresInHours = 24 // Default to 24 hours
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 1. Security Check: Only admins can generate invite links
	myself, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil || myself.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = encoder.Encode(errorResponse("Only admins can generate invite links"))
		return
	}

	// 2. Generate a secure, 12-character URL-safe token
	b := make([]byte, 9) // 9 random bytes
	rand.Read(b)
	// Base64 URLEncoding replaces + and / with - and _ so it doesn't break URLs
	token := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
	expiresAt := time.Now().Add(time.Hour * time.Duration(req.ExpiresInHours))

	// 3. Save to Database
	invite, err := server.store.CreateConversationInvite(r.Context(), db.CreateConversationInviteParams{
		Token:          token,
		ConversationID: conversationID,
		CreatedBy:      authPayload.UserID,
		MaxUses:        req.MaxUses,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = encoder.Encode(inviteResponse{
		Token:          invite.Token,
		ConversationID: invite.ConversationID,
		MaxUses:        invite.MaxUses,
		ExpiresAt:      invite.ExpiresAt.Time,
	})
}

// consumeInvite processes an invitation link click and adds the user to the group
func (server *Server) consumeInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	encoder := json.NewEncoder(w)

	if token == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Token is required"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 1. Fetch Invite first to check if user is already in the group
	// (so we don't burn a 1-time link if they just accidentally clicked it again)
	invite, err := server.store.GetConversationInvite(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			_ = encoder.Encode(errorResponse("Invite link is invalid or expired"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// Check if already in group
	_, err = server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: invite.ConversationID,
		UserID:         authPayload.UserID,
	})
	if err == nil {
		// Already in the group, just return success without consuming the token
		w.WriteHeader(http.StatusOK)
		_ = encoder.Encode(map[string]interface{}{
			"message":        "Already a member",
			"conversationId": invite.ConversationID,
		})
		return
	}

	// 2. Consume the Invite (Atomic Update)
	consumedInvite, err := server.store.ConsumeConversationInvite(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusForbidden)
			_ = encoder.Encode(errorResponse("Invite link has expired or reached its maximum uses"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	// 3. Add Participant as "member"
	_, err = server.store.AddParticipant(r.Context(), db.AddParticipantParams{
		ConversationID: consumedInvite.ConversationID,
		UserID:         authPayload.UserID,
		Role:           "member",
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	user, err := server.store.GetUserById(r.Context(), authPayload.UserID)
	if err == nil {
		systemMsgContent := user.Username + " has joined the chat."
		sysMessage, err := server.store.CreateMessage(r.Context(), db.CreateMessageParams{
			ConversationID: consumedInvite.ConversationID,
			SenderID:       authPayload.UserID,
			Content:        "SYSTEM_EVENT:" + systemMsgContent,
		})

		if err == nil {
			participants, _ := server.store.GetConversationParticipants(r.Context(), consumedInvite.ConversationID)

			userProfile := &userProfileResponse{
				Username:  user.Username,
				FirstName: user.FirstName,
				LastName:  user.LastName,
				AvatarUrl: user.AvatarUrl.String,
			}

			wsResponse := newMessageResponse(sysMessage, userProfile, nil)
			go server.hub.Broadcast(participants, wsResponse)
		} else {
			log.Printf("Failed to create system join message: %v", err)
		}
	} else {
		log.Printf("Failed to fetch user profile for join message: %v", err)
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(map[string]interface{}{
		"message":        "Successfully joined the conversation",
		"conversationId": consumedInvite.ConversationID,
	})
}

// getMyRole returns the current user's role in the specified conversation
func (server *Server) getMyRole(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	participant, err := server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})

	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResponse("Participant not found"))
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"role": participant.Role})
}

func (server *Server) checkInviteStatus(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)
	encoder := json.NewEncoder(w)

	invite, err := server.store.GetConversationInvite(r.Context(), token)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		encoder.Encode(errorResponse("Invite not found or expired"))
		return
	}

	_, err = server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: invite.ConversationID,
		UserID:         authPayload.UserID,
	})

	isMember := err == nil

	w.WriteHeader(http.StatusOK)
	encoder.Encode(map[string]interface{}{
		"isMember":       isMember,
		"conversationId": invite.ConversationID,
	})
}

func (server *Server) globalSearch(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("search")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	encoder := json.NewEncoder(w)

	if page < 1 {
		page = 1
	}
	if limit < 5 {
		limit = 5
	}
	if limit > 100 {
		limit = 100
	}

	arg := db.GlobalSearchParams{
		SearchQuery: searchQuery,
		Limit:       int32(limit),
		Offset:      int32((page - 1) * limit),
	}

	results, err := server.store.GlobalSearch(r.Context(), arg)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse(InternalServerErrorMsg))
		return
	}

	res := make([]globalSearchResponse, len(results))
	for i, row := range results {
		res[i] = globalSearchResponse{
			ID:   row.ID,
			Type: row.ResultType,
		}

		if row.ResultType == "user" {
			res[i].Username = row.Username
			res[i].FirstName = row.FirstName
			res[i].LastName = row.LastName
			res[i].AvatarUrl = row.AvatarUrl.String
		} else if row.ResultType == "channel" {
			// In our SQL, we mapped the channel's name to the 'search_name' column
			res[i].Name = row.SearchName
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(res)
}

func (server *Server) getChatSummary(w http.ResponseWriter, r *http.Request) {
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	encoder := json.NewEncoder(w)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = encoder.Encode(errorResponse("Invalid conversation ID"))
		return
	}

	authPayload := r.Context().Value(authorizationPayloadKey).(*util.TokenPayload)

	// 1. Security Check: Make sure the user is actually in this chat
	_, err = server.store.GetParticipant(r.Context(), db.GetParticipantParams{
		ConversationID: conversationID,
		UserID:         authPayload.UserID,
	})
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = encoder.Encode(errorResponse("You don't have access to this chat"))
		return
	}

	// 2. Fetch the last 50 messages from the database
	messages, err := server.store.GetConversationMessages(r.Context(), db.GetConversationMessagesParams{
		ConversationID: conversationID,
		Limit:          50,
		Offset:         0,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse("Failed to fetch messages"))
		return
	}

	if len(messages) == 0 {
		w.WriteHeader(http.StatusOK)
		_ = encoder.Encode(map[string]string{"summary": "Not enough messages to summarize yet!"})
		return
	}

	// 3. Format the messages into a clean transcript for the AI
	var transcript string
	// Iterate backwards so the oldest message is at the top of the transcript
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		// Skip system events
		if strings.HasPrefix(msg.Content, "SYSTEM_EVENT:") {
			continue
		}
		transcript += fmt.Sprintf("[%s]: %s\n", msg.SenderFirstName+" "+msg.SenderLastName, msg.Content)
	}

	// 4. Call the OpenAI API
	prompt := "You are a helpful AI assistant. Read the following chat transcript and provide a brief, easy-to-read summary in 2-3 bullet points. Focus on the main topics discussed and any decisions made."

	res, err := server.aiClient.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: prompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: "Transcript:\n" + transcript,
				},
			},
		},
	)

	if err != nil {
		log.Printf("OpenAI API error: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = encoder.Encode(errorResponse("Failed to generate AI summary"))
		return
	}

	summary := res.Choices[0].Message.Content

	w.WriteHeader(http.StatusOK)
	_ = encoder.Encode(map[string]string{"summary": summary})
}
