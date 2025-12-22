-- name: CreateUser :one
INSERT INTO users (
    username,
    email,
    password_hash,
    first_name,
    last_name
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1 LIMIT 1;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = $1 LIMIT 1;

-- name: GetUserById :one
SELECT * FROM users
WHERE id = $1 LIMIT 1;

-- name: UpdateUser :one
UPDATE users
SET
    first_name = COALESCE(sqlc.narg('first_name'), first_name),
    last_name = COALESCE(sqlc.narg('last_name'), last_name),
    bio = COALESCE(sqlc.narg('bio'), bio),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdatePassword :exec
UPDATE users
SET
    password_hash = $2,
    updated_at = now()
WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
WHERE
    -- If $1 (query) is empty, match everything (List All).
    -- If $1 has text, search username OR first/last names.
    ($1::text = '' OR
    username ILIKE '%' || $1 || '%' OR
    first_name ILIKE '%' || $1 || '%' OR
    last_name ILIKE '%' || $1 || '%')
ORDER BY username
LIMIT $2
OFFSET $3;