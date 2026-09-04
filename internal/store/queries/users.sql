-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CountAdminUsers :one
SELECT count(*) FROM users WHERE is_admin = true;

-- name: CreateAdminUser :one
INSERT INTO users (email, password_hash, display_name, is_admin)
VALUES ($1, $2, $3, true)
RETURNING *;

-- name: CreateGuestUser :one
INSERT INTO users (email, password_hash, display_name, is_guest)
VALUES ($1, $2, $3, true)
RETURNING *;
