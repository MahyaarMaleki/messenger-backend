package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Custom key type to avoid collisions in the context map
type contextKey string

const (
	authorizationHeaderKey  = "authorization"
	authorizationTypeBearer = "bearer"
	authorizationPayloadKey = contextKey("authorization_payload")
)

func (server *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoder := json.NewEncoder(w)

		// Get the Authorization header
		authorizationHeader := r.Header.Get(authorizationHeaderKey)
		if len(authorizationHeader) == 0 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = encoder.Encode(errorResponse("Authorization header is not provided"))
			return
		}

		// Parse the header (Format: "Bearer <token>")
		fields := strings.Fields(authorizationHeader)
		if len(fields) < 2 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = encoder.Encode(errorResponse("Invalid authorization header format"))
			return
		}

		authorizationType := strings.ToLower(fields[0])
		if authorizationType != authorizationTypeBearer {
			w.WriteHeader(http.StatusUnauthorized)
			_ = encoder.Encode(errorResponse("Unsupported authorization type"))
			return
		}

		accessToken := fields[1]

		// Verify Token
		payload, err := server.tokenMaker.Verify(accessToken)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = encoder.Encode(errorResponse(err.Error())) // Verify returns "expired" or "invalid"
			return
		}

		// Store token payload in Context
		ctx := context.WithValue(r.Context(), authorizationPayloadKey, payload)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
