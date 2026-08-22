package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

// APIKeyLookup resolves a raw API key's hash to its owning user, per the
// ApiKeyAuth security scheme (api/openapi.yaml). Backed by the api_keys
// table (migration 0008) via PostgresAPIKeyLookup in production.
type APIKeyLookup interface {
	GetUserIDByKeyHash(ctx context.Context, hash string) (userID int64, found bool, err error)
	TouchLastUsed(ctx context.Context, hash string) error
}

// AuthMiddleware enforces the JWT-or-ApiKey security scheme from
// 架构设计文档 6.5. Mount it on protected route groups only — /auth/register
// and /auth/login stay outside it (security: [] in the API contract).
// A missing or unparseable Authorization header, or a malformed/wrong-type
// token, is 401 + 20001; a validly-issued but expired JWT is 401 + 20002.
func AuthMiddleware(issuer *auth.TokenIssuer, apiKeys APIKeyLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")

			switch {
			case strings.HasPrefix(header, "Bearer "):
				token := strings.TrimPrefix(header, "Bearer ")
				userID, err := issuer.VerifyAccessToken(token)
				if err != nil {
					writeTokenError(w, r, err)
					return
				}
				next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))

			case strings.HasPrefix(header, "ApiKey "):
				rawKey := strings.TrimPrefix(header, "ApiKey ")
				hash := auth.HashAPIKey(rawKey)
				userID, found, err := apiKeys.GetUserIDByKeyHash(r.Context(), hash)
				if err != nil || !found {
					writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "invalid API key")
					return
				}
				_ = apiKeys.TouchLastUsed(r.Context(), hash)
				next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))

			default:
				writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "missing or malformed Authorization header")
			}
		})
	}
}

func writeTokenError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrTokenExpired) {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenExpired, "token expired")
		return
	}
	writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "invalid token")
}

// RequireOwner returns 403 + 20003 (via ok=false) unless the authenticated
// user in ctx matches resourceOwnerID. Every handler that reads a
// user-owned resource must call this before returning it — the uniform
// checkpoint spec-04 asks for instead of ad hoc ownership checks scattered
// across handlers.
func RequireOwner(w http.ResponseWriter, r *http.Request, resourceOwnerID int64) bool {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID != resourceOwnerID {
		writeErr(w, r, http.StatusForbidden, ErrForbidden, "you do not have access to this resource")
		return false
	}
	return true
}
