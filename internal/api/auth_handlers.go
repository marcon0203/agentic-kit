package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/iam"
)

// AuthHandlers is the HTTP transport for POST /auth/register and
// /auth/login, per api/openapi.yaml's AuthResult contract.
type AuthHandlers struct {
	svc *iam.Service
}

func NewAuthHandlers(svc *iam.Service) *AuthHandlers { return &AuthHandlers{svc: svc} }

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
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
}

type authResultDTO struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	User         userDTO `json:"user"`
}

func toAuthResultDTO(s iam.Session) authResultDTO {
	return authResultDTO{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		User: userDTO{
			ID: strconv.FormatInt(s.User.ID, 10), Email: s.User.Email,
			DisplayName: s.User.DisplayName, IsAdmin: s.User.IsAdmin, CreatedAt: s.User.CreatedAt,
		},
	}
}

// Register handles POST /auth/register.
func (h *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	session, err := h.svc.Register(r.Context(), iam.RegisterCommand{
		Email: req.Email, Password: req.Password, DisplayName: req.DisplayName,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toAuthResultDTO(session))
}

// Login handles POST /auth/login.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	session, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toAuthResultDTO(session))
}
