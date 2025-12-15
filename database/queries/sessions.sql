-- name: CreateSession :one
INSERT INTO sessions (
    id,
    user_id,
    refresh_token,
    user_agent,
    client_ip,
    is_blocked,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions
WHERE id = $1 LIMIT 1;

-- name: ListSessionsByUser :many
-- ListSessionsByUser Shows the user all their active logins excluding expired sessions
SELECT * FROM sessions
WHERE
    user_id = $1
    AND expires_at > now()
ORDER BY created_at DESC;

-- name: BlockSession :exec
-- BlockSession acts as a "Log out this device" button and makes the token invalid immediately
UPDATE sessions
SET is_blocked = true
WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = $1;

-- name: DeleteExpiredSessions :exec
-- DeleteExpiredSessions is for an optional background Cron Job (cleanup) to remove old sessions from the database
DELETE FROM sessions
WHERE expires_at < now();