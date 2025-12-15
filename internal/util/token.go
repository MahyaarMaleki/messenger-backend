package util

import (
	"errors"
	"fmt"
	"time"

	"github.com/aead/chacha20poly1305"
	"github.com/google/uuid"
	"github.com/o1egl/paseto"
)

var (
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidToken = errors.New("token is invalid")
)

// PasetoMaker handles token creation and validation
type PasetoMaker struct {
	paseto       *paseto.V2
	symmetricKey []byte
}

// TokenPayload contains the data inside the token
type TokenPayload struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"userId"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiredAt time.Time `json:"expiredAt"`
}

// NewPasetoMaker creates a new Paseto instance
func NewPasetoMaker(key string) (*PasetoMaker, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("invalid key size: must be exactly %d characters", chacha20poly1305.KeySize)
	}

	return &PasetoMaker{
		paseto:       paseto.NewV2(),
		symmetricKey: []byte(key),
	}, nil
}

// Create generates a new encrypted token
func (maker *PasetoMaker) Create(userID uuid.UUID, duration time.Duration) (string, *TokenPayload, error) {
	tokenID, err := uuid.NewRandom()
	if err != nil {
		return "", nil, err
	}

	payload := &TokenPayload{
		ID:        tokenID,
		UserID:    userID,
		IssuedAt:  time.Now(),
		ExpiredAt: time.Now().Add(duration),
	}

	// Encrypt the payload
	token, err := maker.paseto.Encrypt(maker.symmetricKey, payload, nil)

	return token, payload, err
}

// Verify checks if the token is valid and returns the payload
func (maker *PasetoMaker) Verify(token string) (*TokenPayload, error) {
	payload := &TokenPayload{}

	// Decrypt the payload
	err := maker.paseto.Decrypt(token, maker.symmetricKey, payload, nil)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Check expiration
	if time.Now().After(payload.ExpiredAt) {
		return nil, ErrExpiredToken
	}

	return payload, nil
}
