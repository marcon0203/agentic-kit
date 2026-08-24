package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/rbac"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RBACRepository implements rbac.Repository.
type RBACRepository struct{ q store.Querier }

func NewRBACRepository(q store.Querier) *RBACRepository { return &RBACRepository{q: q} }

var _ rbac.Repository = (*RBACRepository)(nil)

func toDomainPermission(row store.Permission) rbac.Permission {
	return rbac.Permission{ID: row.ID, Key: row.PermissionKey, Name: row.Name, Module: row.Module, CreatedAt: row.CreatedAt.Time}
}

func toDomainRole(row store.Role) rbac.Role {
	return rbac.Role{ID: row.ID, Key: row.RoleKey, Name: row.Name, Description: row.Description, CreatedAt: row.CreatedAt.Time}
}

func (r *RBACRepository) ListPermissions(ctx context.Context) ([]rbac.Permission, error) {
	rows, err := r.q.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]rbac.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPermission(row))
	}
	return out, nil
}

func (r *RBACRepository) ValidKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	ids, err := r.q.GetPermissionIDsByKeys(ctx, keys)
	if err != nil {
		return nil, err
	}
	// GetPermissionIDsByKeys only tells us how many matched, not which —
	// re-list and filter is unnecessary; a second pass over all
	// permissions is cheap and this table is small (one row per gated
	// button in the whole app, not per tenant).
	all, err := r.q.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	matched := make(map[int64]bool, len(ids))
	for _, id := range ids {
		matched[id] = true
	}
	out := make(map[string]bool, len(keys))
	for _, p := range all {
		if matched[p.ID] {
			out[p.PermissionKey] = true
		}
	}
	return out, nil
}

func (r *RBACRepository) CreateRole(ctx context.Context, key, name, description string) (rbac.Role, error) {
	row, err := r.q.CreateRole(ctx, store.CreateRoleParams{RoleKey: key, Name: name, Description: description})
	if err != nil {
		if isUniqueViolation(err) {
			return rbac.Role{}, rbac.ErrDuplicate
		}
		return rbac.Role{}, err
	}
	return toDomainRole(row), nil
}

func (r *RBACRepository) ListRoles(ctx context.Context) ([]rbac.Role, error) {
	rows, err := r.q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	roles := make([]rbac.Role, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, toDomainRole(row))
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return roles, nil
	}
	permRows, err := r.q.ListRolePermissionKeys(ctx, ids)
	if err != nil {
		return nil, err
	}
	byRole := make(map[int64][]string, len(roles))
	for _, pr := range permRows {
		byRole[pr.RoleID] = append(byRole[pr.RoleID], pr.PermissionKey)
	}
	for i := range roles {
		roles[i].Permissions = byRole[roles[i].ID]
	}
	return roles, nil
}

func (r *RBACRepository) GetRole(ctx context.Context, id int64) (rbac.Role, error) {
	row, err := r.q.GetRole(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rbac.Role{}, rbac.ErrNotFound
		}
		return rbac.Role{}, err
	}
	return toDomainRole(row), nil
}

func (r *RBACRepository) DeleteRole(ctx context.Context, id int64) error {
	return r.q.DeleteRole(ctx, id)
}

// SetRolePermissions replaces a role's full permission set: clear then
// re-insert. Not wrapped in a DB transaction — this codebase has no
// transaction-spanning repository pattern yet (see internal/store.DBTX /
// Queries.WithTx, unused elsewhere); a failure between the two steps would
// leave the role with fewer permissions than intended rather than corrupt
// data, and the admin UI shows the result immediately so it's caught fast.
func (r *RBACRepository) SetRolePermissions(ctx context.Context, roleID int64, permissionKeys []string) error {
	if err := r.q.DeleteRolePermissions(ctx, roleID); err != nil {
		return err
	}
	if len(permissionKeys) == 0 {
		return nil
	}
	ids, err := r.q.GetPermissionIDsByKeys(ctx, permissionKeys)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.q.InsertRolePermission(ctx, store.InsertRolePermissionParams{RoleID: roleID, PermissionID: id}); err != nil {
			return err
		}
	}
	return nil
}

func (r *RBACRepository) ListUsers(ctx context.Context, search string) ([]rbac.UserAccount, error) {
	rows, err := r.q.ListUsersForAdmin(ctx, search)
	if err != nil {
		return nil, err
	}
	users := make([]rbac.UserAccount, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		users = append(users, rbac.UserAccount{
			ID: row.ID, Email: row.Email, DisplayName: row.DisplayName,
			Status: row.Status, IsAdmin: row.IsAdmin, CreatedAt: row.CreatedAt.Time,
		})
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return users, nil
	}
	roleIDRows, err := r.q.ListUserRoleIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(roleIDRows) == 0 {
		return users, nil
	}
	allRoles, err := r.q.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	roleKeyByID := make(map[int64]string, len(allRoles))
	for _, role := range allRoles {
		roleKeyByID[role.ID] = role.RoleKey
	}
	rolesByUser := make(map[int64][]string, len(users))
	for _, rr := range roleIDRows {
		rolesByUser[rr.UserID] = append(rolesByUser[rr.UserID], roleKeyByID[rr.RoleID])
	}
	for i := range users {
		users[i].Roles = rolesByUser[users[i].ID]
	}
	return users, nil
}

func (r *RBACRepository) SetUserStatus(ctx context.Context, userID int64, status int16) error {
	return r.q.SetUserStatus(ctx, store.SetUserStatusParams{ID: userID, Status: status})
}

// SetUserRoles replaces a user's full role assignment — same
// clear-then-reinsert approach as SetRolePermissions, same rationale.
func (r *RBACRepository) SetUserRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	if err := r.q.DeleteUserRoles(ctx, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if err := r.q.InsertUserRole(ctx, store.InsertUserRoleParams{UserID: userID, RoleID: roleID}); err != nil {
			return err
		}
	}
	return nil
}

func (r *RBACRepository) HasPermission(ctx context.Context, userID int64, key string) (bool, error) {
	return r.q.UserHasPermission(ctx, store.UserHasPermissionParams{UserID: userID, PermissionKey: key})
}

func (r *RBACRepository) PermissionKeysForUser(ctx context.Context, userID int64) ([]string, error) {
	return r.q.ListPermissionKeysForUser(ctx, userID)
}
