package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/marcon0203/agentic-kit/internal/domain/rbac"
)

// RBACHandlers is 系统配置 → 用户管理 / 角色权限's HTTP transport.
type RBACHandlers struct {
	svc *rbac.Service
}

func NewRBACHandlers(svc *rbac.Service) *RBACHandlers { return &RBACHandlers{svc: svc} }

type permissionDTO struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Module string `json:"module"`
}

func toPermissionDTO(p rbac.Permission) permissionDTO {
	return permissionDTO{Key: p.Key, Name: p.Name, Module: p.Module}
}

type roleDTO struct {
	ID             string    `json:"id"`
	Key            string    `json:"key"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	PermissionKeys []string  `json:"permission_keys"`
	CreatedAt      time.Time `json:"created_at"`
}

func toRoleDTO(r rbac.Role) roleDTO {
	keys := r.Permissions
	if keys == nil {
		keys = []string{}
	}
	return roleDTO{
		ID: strconv.FormatInt(r.ID, 10), Key: r.Key, Name: r.Name, Description: r.Description,
		PermissionKeys: keys, CreatedAt: r.CreatedAt,
	}
}

type userAccountDTO struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Status      int16     `json:"status"`
	IsAdmin     bool      `json:"is_admin"`
	RoleKeys    []string  `json:"role_keys"`
	CreatedAt   time.Time `json:"created_at"`
}

func toUserAccountDTO(u rbac.UserAccount) userAccountDTO {
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	return userAccountDTO{
		ID: strconv.FormatInt(u.ID, 10), Email: u.Email, DisplayName: u.DisplayName,
		Status: u.Status, IsAdmin: u.IsAdmin, RoleKeys: roles, CreatedAt: u.CreatedAt,
	}
}

type myPermissionsDTO struct {
	IsAdmin     bool     `json:"is_admin"`
	Permissions []string `json:"permissions"`
}

// MyPermissions handles GET /me/permissions — the frontend's button-gating
// source of truth for the current session. Any authenticated user, not
// just admins: everyone needs to know what they themselves can do.
func (h *RBACHandlers) MyPermissions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	isAdmin, keys, err := h.svc.MyPermissions(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, r, http.StatusOK, myPermissionsDTO{IsAdmin: isAdmin, Permissions: keys})
}

// ListPermissions handles GET /permissions.
func (h *RBACHandlers) ListPermissions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	perms, err := h.svc.ListPermissions(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]permissionDTO, 0, len(perms))
	for _, p := range perms {
		items = append(items, toPermissionDTO(p))
	}
	writeJSON(w, r, http.StatusOK, items)
}

// ListRoles handles GET /roles.
func (h *RBACHandlers) ListRoles(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	roles, err := h.svc.ListRoles(r.Context(), userID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]roleDTO, 0, len(roles))
	for _, role := range roles {
		items = append(items, toRoleDTO(role))
	}
	writeJSON(w, r, http.StatusOK, items)
}

type createRoleRequest struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permission_keys"`
}

// CreateRole handles POST /roles.
func (h *RBACHandlers) CreateRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req createRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	created, err := h.svc.CreateRole(r.Context(), userID, req.Key, req.Name, req.Description, req.PermissionKeys)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toRoleDTO(created))
}

type setRolePermissionsRequest struct {
	PermissionKeys []string `json:"permission_keys"`
}

// UpdateRolePermissions handles PATCH /roles/{id}/permissions.
func (h *RBACHandlers) UpdateRolePermissions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	roleID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req setRolePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	updated, err := h.svc.SetRolePermissions(r.Context(), userID, roleID, req.PermissionKeys)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toRoleDTO(updated))
}

// DeleteRole handles DELETE /roles/{id}.
func (h *RBACHandlers) DeleteRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	roleID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteRole(r.Context(), userID, roleID); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

// ListUsers handles GET /users?search=.
func (h *RBACHandlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	users, err := h.svc.ListUsers(r.Context(), userID, r.URL.Query().Get("search"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]userAccountDTO, 0, len(users))
	for _, u := range users {
		items = append(items, toUserAccountDTO(u))
	}
	writeJSON(w, r, http.StatusOK, items)
}

// UpdateUserStatus handles PATCH /users/{id}/status.
func (h *RBACHandlers) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	targetID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req setStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	if err := h.svc.SetUserStatus(r.Context(), userID, targetID, req.Status == 1); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}

type setUserRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

// UpdateUserRoles handles PATCH /users/{id}/roles.
func (h *RBACHandlers) UpdateUserRoles(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	targetID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req setUserRolesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}
	roleIDs := make([]int64, 0, len(req.RoleIDs))
	for _, s := range req.RoleIDs {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid role id")
			return
		}
		roleIDs = append(roleIDs, id)
	}
	if err := h.svc.SetUserRoles(r.Context(), userID, targetID, roleIDs); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, nil)
}
