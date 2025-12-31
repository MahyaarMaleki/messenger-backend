package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/mahyaarmaleki/messenger-backend/internal/db"
)

type (
	createUserRequest struct {
		Username  string `json:"username" validate:"required,min=3,max=30"`
		Password  string `json:"password" validate:"required"`
		Email     string `json:"email" validate:"required,email"`
		FirstName string `json:"firstName" validate:"required,max=50"`
		LastName  string `json:"lastName" validate:"required,max=50"`
	}

	loginUserRequest struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}

	createSessionRequest struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}

	renewAccessTokenRequest struct {
		RefreshToken string `json:"refreshToken" validate:"required"`
	}

	revokeSessionRequest struct {
		ID uuid.UUID `json:"id" validate:"required"`
	}

	updateUserRequest struct {
		FirstName *string `json:"firstName"`
		LastName  *string `json:"lastName"`
		Bio       *string `json:"bio"`
		AvatarUrl *string `json:"avatarUrl"`
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

	loginUserResponse struct {
		SessionID             uuid.UUID    `json:"sessionId"`
		AccessToken           string       `json:"accessToken"`
		AccessTokenExpiresAt  time.Time    `json:"accessTokenExpiresAt"`
		RefreshToken          string       `json:"refreshToken"`
		RefreshTokenExpiresAt time.Time    `json:"refreshTokenExpiresAt"`
		User                  userResponse `json:"user"`
	}
)

func newUserResponse(user db.User) userResponse {
	return userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
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
