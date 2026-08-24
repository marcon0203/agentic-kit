-- name: ListPermissions :many
SELECT id, permission_key, name, module, created_at
FROM permissions
ORDER BY module ASC, id ASC;

-- name: GetPermissionIDsByKeys :many
SELECT id FROM permissions WHERE permission_key = ANY(sqlc.arg(keys)::text[]);

-- name: CreateRole :one
INSERT INTO roles (role_key, name, description)
VALUES ($1, $2, $3)
RETURNING id, role_key, name, description, created_at;

-- name: ListRoles :many
SELECT id, role_key, name, description, created_at
FROM roles
ORDER BY created_at ASC;

-- name: GetRole :one
SELECT id, role_key, name, description, created_at
FROM roles
WHERE id = $1;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = $1;

-- name: ListRolePermissionKeys :many
SELECT r.role_id, p.permission_key
FROM role_permissions r
JOIN permissions p ON p.id = r.permission_id
WHERE r.role_id = ANY(sqlc.arg(role_ids)::bigint[]);

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions WHERE role_id = $1;

-- name: InsertRolePermission :exec
INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteUserRoles :exec
DELETE FROM user_roles WHERE user_id = $1;

-- name: InsertUserRole :exec
INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListRolesForUser :many
SELECT r.id, r.role_key, r.name, r.description, r.created_at
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = $1
ORDER BY r.created_at ASC;

-- name: ListUserRoleIDs :many
SELECT role_id, user_id FROM user_roles WHERE user_id = ANY(sqlc.arg(user_ids)::bigint[]);

-- name: ListPermissionKeysForUser :many
SELECT DISTINCT p.permission_key
FROM user_roles ur
JOIN role_permissions rp ON rp.role_id = ur.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE ur.user_id = $1;

-- name: UserHasPermission :one
SELECT EXISTS (
    SELECT 1
    FROM user_roles ur
    JOIN role_permissions rp ON rp.role_id = ur.role_id
    JOIN permissions p ON p.id = rp.permission_id
    WHERE ur.user_id = $1 AND p.permission_key = $2
) AS has_permission;

-- name: ListUsersForAdmin :many
SELECT id, email, display_name, status, is_admin, created_at
FROM users
WHERE (sqlc.arg(search)::text = '' OR email ILIKE '%' || sqlc.arg(search)::text || '%' OR display_name ILIKE '%' || sqlc.arg(search)::text || '%')
ORDER BY created_at ASC;

-- name: SetUserStatus :exec
UPDATE users SET status = $2 WHERE id = $1;
