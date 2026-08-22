package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/auth"
)

// AuthHandlers implements POST /auth/register and /auth/login per
// api/openapi.yaml's AuthResult contract.
type AuthHandlers struct {
	Users  AuthUserStore
	Tokens *auth.TokenIssuer
}

func NewAuthHandlers(users AuthUserStore, tokens *auth.TokenIssuer) *AuthHandlers {
	return &AuthHandlers{Users: users, Tokens: tokens}
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userDTO matches components.schemas.User in api/openapi.yaml: ID is a
// string on the wire to avoid JS Number precision loss on large IDs.
type userDTO struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type authResultDTO struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	User         userDTO `json:"user"`
}

// Register handles POST /auth/register.
func (h *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	var details []FieldError
	if req.Email == "" {
		details = append(details, FieldError{Field: "email", Message: "required"})
	}
	if len(req.Password) < 8 {
		details = append(details, FieldError{Field: "password", Message: "must be at least 8 characters"})
	}
	if req.DisplayName == "" {
		details = append(details, FieldError{Field: "display_name", Message: "required"})
	}
	if len(details) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "validation failed", details)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	user, err := h.Users.CreateUser(r.Context(), req.Email, hash, req.DisplayName)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			writeErr(w, r, http.StatusConflict, ErrEmailAlreadyRegistered, "email already registered")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	h.respondWithTokens(w, r, http.StatusCreated, user)
}

// Login handles POST /auth/login.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	user, found, err := h.Users.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	// Verify against a hash either way, valid or a fixed dummy, so a
	// nonexistent-email response takes the same time as a wrong-password
	// one — an unknown-email fast path is a user-enumeration oracle.
	hashToCheck := user.PasswordHash
	if !found {
		hashToCheck = dummyPasswordHash
	}
	ok, err := auth.VerifyPassword(req.Password, hashToCheck)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if !found || !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrInvalidCredentials, "invalid email or password")
		return
	}

	h.respondWithTokens(w, r, http.StatusOK, user)
}

// dummyPasswordHash is a valid argon2id-encoded hash of a value nobody will
// enter, used only to keep Login's timing constant when no such user
// exists.
var dummyPasswordHash = mustHash("this-is-not-a-real-password-anyones-account-has")

func mustHash(s string) string {
	h, err := auth.HashPassword(s)
	if err != nil {
		panic(err)
	}
	return h
}

func (h *AuthHandlers) respondWithTokens(w http.ResponseWriter, r *http.Request, status int, user AuthUser) {
	access, err := h.Tokens.IssueAccessToken(user.ID)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	refresh, err := h.Tokens.IssueRefreshToken(user.ID)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	writeJSON(w, r, status, authResultDTO{
		AccessToken:  access,
		RefreshToken: refresh,
		User: userDTO{
			ID:          strconv.FormatInt(user.ID, 10),
			Email:       user.Email,
			DisplayName: user.DisplayName,
			CreatedAt:   user.CreatedAt,
		},
	})
}
