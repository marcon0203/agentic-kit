package rbac

import (
	"context"
	"errors"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

var ErrNotFound = errors.New("rbac: not found")
var ErrDuplicate = errors.New("rbac: already exists")

// Repository is the persistence port.
type Repository interface {
	ListPermissions(ctx context.Context) ([]Permission, error)
	// ValidKeys reports which of the given permission keys actually exist
	// in the seeded catalog — used to reject a role assignment that
	// references a typo'd or retired key.
	ValidKeys(ctx context.Context, keys []string) (map[string]bool, error)

	CreateRole(ctx context.Context, key, name, description string) (Role, error)
	ListRoles(ctx context.Context) ([]Role, error)
	GetRole(ctx context.Context, id int64) (Role, error)
	DeleteRole(ctx context.Context, id int64) error
	SetRolePermissions(ctx context.Context, roleID int64, permissionKeys []string) error

	ListUsers(ctx context.Context, search string) ([]UserAccount, error)
	SetUserStatus(ctx context.Context, userID int64, status int16) error
	SetUserRoles(ctx context.Context, userID int64, roleIDs []int64) error

	HasPermission(ctx context.Context, userID int64, key string) (bool, error)
	PermissionKeysForUser(ctx context.Context, userID int64) ([]string, error)
}

type AdminDirectory interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

// Service is 系统配置 → 用户管理 / 角色权限's application service. Every
// write here requires true is_admin, not just a granted permission —
// letting a Role holder edit Roles would let them grant themselves more,
// so role/permission/user administration stays a superadmin-only surface.
type Service struct {
	repo   Repository
	admins AdminDirectory
}

func NewService(repo Repository, admins AdminDirectory) *Service {
	return &Service{repo: repo, admins: admins}
}

// HasPermission is the check primitive other domain services call (e.g.
// modelcatalog's requireAccess) — not admin-gated itself, since any caller
// needs to be able to ask "does this user have X" about themselves.
func (s *Service) HasPermission(ctx context.Context, userID int64, key string) (bool, error) {
	return s.repo.HasPermission(ctx, userID, key)
}

// MyPermissions is the frontend's button-gating source of truth: the
// permission keys the caller holds via their Roles, plus whether they're a
// superadmin (which implicitly grants everything is_admin bypasses on the
// backend). No admin gate — every logged-in user can ask this about
// themselves.
func (s *Service) MyPermissions(ctx context.Context, userID int64) (isAdmin bool, keys []string, err error) {
	isAdmin, err = s.admins.IsAdmin(ctx, userID)
	if err != nil {
		return false, nil, domain.Internal(err)
	}
	keys, err = s.repo.PermissionKeysForUser(ctx, userID)
	if err != nil {
		return false, nil, domain.Internal(err)
	}
	return isAdmin, keys, nil
}

func (s *Service) ListPermissions(ctx context.Context, userID int64) ([]Permission, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	perms, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return perms, nil
}

func (s *Service) ListRoles(ctx context.Context, userID int64) ([]Role, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, domain.Internal(err)
	}
	return roles, nil
}

func (s *Service) CreateRole(ctx context.Context, userID int64, key, name, description string, permissionKeys []string) (Role, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Role{}, err
	}

	key = strings.TrimSpace(key)
	name = strings.TrimSpace(name)
	var fields []domain.FieldError
	if key == "" {
		fields = append(fields, domain.FieldError{Field: "key", Reason: "required"})
	}
	if name == "" {
		fields = append(fields, domain.FieldError{Field: "name", Reason: "required"})
	}
	if len(fields) > 0 {
		return Role{}, domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(fields...)
	}
	if err := s.validatePermissionKeys(ctx, permissionKeys); err != nil {
		return Role{}, err
	}

	created, err := s.repo.CreateRole(ctx, key, name, description)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return Role{}, domain.Conflict(domain.CodeRoleKeyDuplicate, "该角色 key 已存在")
		}
		return Role{}, domain.Internal(err)
	}
	if len(permissionKeys) > 0 {
		if err := s.repo.SetRolePermissions(ctx, created.ID, permissionKeys); err != nil {
			return Role{}, domain.Internal(err)
		}
		created.Permissions = permissionKeys
	}
	return created, nil
}

func (s *Service) SetRolePermissions(ctx context.Context, userID, roleID int64, permissionKeys []string) (Role, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return Role{}, err
	}
	role, err := s.repo.GetRole(ctx, roleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Role{}, domain.NotFound(domain.CodeRoleNotFound, "角色不存在")
		}
		return Role{}, domain.Internal(err)
	}
	if err := s.validatePermissionKeys(ctx, permissionKeys); err != nil {
		return Role{}, err
	}
	if err := s.repo.SetRolePermissions(ctx, roleID, permissionKeys); err != nil {
		return Role{}, domain.Internal(err)
	}
	role.Permissions = permissionKeys
	return role, nil
}

func (s *Service) DeleteRole(ctx context.Context, userID, roleID int64) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if _, err := s.repo.GetRole(ctx, roleID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeRoleNotFound, "角色不存在")
		}
		return domain.Internal(err)
	}
	if err := s.repo.DeleteRole(ctx, roleID); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func (s *Service) ListUsers(ctx context.Context, userID int64, search string) ([]UserAccount, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	users, err := s.repo.ListUsers(ctx, strings.TrimSpace(search))
	if err != nil {
		return nil, domain.Internal(err)
	}
	return users, nil
}

// SetUserStatus enables/disables an account. A caller can never disable
// their own account — that would be a self-lockout with no admin left to
// undo it if they were the only one.
func (s *Service) SetUserStatus(ctx context.Context, userID, targetUserID int64, enabled bool) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if !enabled && targetUserID == userID {
		return domain.Invalid(domain.CodeValidationFailed, "不能停用自己的账号").
			WithDetails(domain.FieldError{Field: "user_id", Reason: "cannot disable self"})
	}
	status := int16(2)
	if enabled {
		status = 1
	}
	if err := s.repo.SetUserStatus(ctx, targetUserID, status); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func (s *Service) SetUserRoles(ctx context.Context, userID, targetUserID int64, roleIDs []int64) error {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return err
	}
	if err := s.repo.SetUserRoles(ctx, targetUserID, roleIDs); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func (s *Service) validatePermissionKeys(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	valid, err := s.repo.ValidKeys(ctx, keys)
	if err != nil {
		return domain.Internal(err)
	}
	var fields []domain.FieldError
	for _, k := range keys {
		if !valid[k] {
			fields = append(fields, domain.FieldError{Field: "permission_keys", Reason: "unknown permission: " + k})
		}
	}
	if len(fields) > 0 {
		return domain.Invalid(domain.CodeValidationFailed, "validation failed").WithDetails(fields...)
	}
	return nil
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	isAdmin, err := s.admins.IsAdmin(ctx, userID)
	if err != nil {
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	if !isAdmin {
		return domain.Forbidden(domain.CodeForbidden, "admin access required")
	}
	return nil
}
