package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
)

type (
	createUserRequest struct {
		Username  string `json:"username" validate:"required,min=3,max=30"`
		Password  string `json:"password" validate:"required,min=8,max=32"`
		Email     string `json:"email" validate:"required,email"`
		FirstName string `json:"firstName" validate:"required,max=50"`
		LastName  string `json:"lastName" validate:"required,max=50"`
	}

	createSessionRequest struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}

	renewAccessTokenRequest struct {
		RefreshToken string `json:"refreshToken" validate:"required"`
	}

	updateUserRequest struct {
		FirstName *string `json:"firstName" validate:"omitempty,max=50"`
		LastName  *string `json:"lastName" validate:"omitempty,max=50"`
		Bio       *string `json:"bio" validate:"omitempty,max=70"`
		AvatarUrl *string `json:"avatarUrl" validate:"omitempty,url"`
	}

	changePasswordRequest struct {
		OldPassword string `json:"oldPassword" validate:"required"`
		NewPassword string `json:"newPassword" validate:"required,min=8,max=32"`
	}

	createConversationRequest struct {
		Name           *string `json:"name" validate:"required_if=Type group,required_if=Type channel,omitempty,min=3"`
		Type           string  `json:"type" validate:"required,oneof=private group channel"`
		TargetUsername *string `json:"targetUsername" validate:"required_if=Type private"`
	}

	attachmentDTO struct {
		URL  string `json:"url" validate:"required"`
		Type string `json:"type" validate:"required"`
		Name string `json:"name" validate:"required"`
	}

	createMessageRequest struct {
		Content     string          `json:"content" validate:"required"`
		Attachments []attachmentDTO `json:"attachments" validate:"omitempty,dive"`
	}

	updateMessageRequest struct {
		Content string `json:"content" validate:"required"`
	}

	addParticipantRequest struct {
		Username string `json:"username" validate:"required"`
	}

	updateConversationRequest struct {
		Name string `json:"name" validate:"required,min=3"`
	}

	messageResponse struct {
		ID             uuid.UUID       `json:"id"`
		ConversationID uuid.UUID       `json:"conversationId"`
		SenderID       uuid.UUID       `json:"senderId"`
		Content        string          `json:"content"`
		Attachments    []attachmentDTO `json:"attachments"`
		CreatedAt      time.Time       `json:"createdAt"`
		UpdatedAt      time.Time       `json:"updatedAt"`
	}

	conversationResponse struct {
		ID               uuid.UUID            `json:"id"`
		Name             string               `json:"name"`
		Type             string               `json:"type"`
		LastMessageAt    time.Time            `json:"lastMessageAt"`
		CreatedAt        time.Time            `json:"createdAt"`
		OtherParticipant *userProfileResponse `json:"otherParticipant,omitempty"`
	}

	renewAccessTokenResponse struct {
		AccessToken          string    `json:"accessToken"`
		AccessTokenExpiresAt time.Time `json:"accessTokenExpiresAt"`
	}

	createSessionResponse struct {
		SessionID             uuid.UUID    `json:"sessionId"`
		AccessToken           string       `json:"accessToken"`
		AccessTokenExpiresAt  time.Time    `json:"accessTokenExpiresAt"`
		RefreshToken          string       `json:"refreshToken"`
		RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt"`
		User                  userResponse `json:"user"`
	}

	// userResponse (Private) - For Register/Login/Me
	userResponse struct {
		ID        uuid.UUID `json:"id"`
		Username  string    `json:"username"`
		Email     string    `json:"email"`
		FirstName string    `json:"firstName"`
		LastName  string    `json:"lastName"`
		Bio       string    `json:"bio"`
		AvatarUrl string    `json:"avatarUrl"`
		CreatedAt time.Time `json:"createdAt"`
	}

	// userProfileResponse (Public) - For Search/Profile View
	userProfileResponse struct {
		Username  string `json:"username"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Bio       string `json:"bio"`
		AvatarUrl string `json:"avatarUrl"`
	}
)

func newUserResponse(user db.User) userResponse {
	return userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Bio:       user.Bio.String,
		AvatarUrl: user.AvatarUrl.String,
		CreatedAt: user.CreatedAt.Time,
	}
}

func newUserProfileResponse(user db.User) userProfileResponse {
	return userProfileResponse{
		Username:  user.Username,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Bio:       user.Bio.String,
		AvatarUrl: user.AvatarUrl.String,
	}
}

func newConversationResponse(c db.Conversation, otherUser *userProfileResponse) conversationResponse {
	displayName := c.Name.String

	if c.Type == "private" {
		if otherUser != nil {
			displayName = otherUser.Username
			// Or: displayName = otherUser.FirstName + " " + otherUser.LastName
		} else {
			// Edge case: User deleted or self-chat
			displayName = "Unknown User"
		}
	}

	return conversationResponse{
		ID:               c.ID,
		Name:             displayName,
		Type:             c.Type,
		LastMessageAt:    c.LastMessageAt.Time,
		CreatedAt:        c.CreatedAt.Time,
		OtherParticipant: otherUser,
	}
}

func newMessageResponse(msg db.Message, attachments []attachmentDTO) messageResponse {
	if attachments == nil {
		attachments = []attachmentDTO{}
	}

	return messageResponse{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		Content:        msg.Content,
		Attachments:    attachments,
		CreatedAt:      msg.CreatedAt.Time,
		UpdatedAt:      msg.UpdatedAt.Time,
	}
}
