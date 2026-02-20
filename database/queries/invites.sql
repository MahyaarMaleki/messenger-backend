-- name: CreateConversationInvite :one
INSERT INTO conversation_invites (
    token,
    conversation_id,
    created_by,
    max_uses,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetConversationInvite :one
SELECT * FROM conversation_invites
WHERE token = $1 LIMIT 1;

-- name: ConsumeConversationInvite :one
UPDATE conversation_invites
SET uses_count = uses_count + 1
WHERE token = $1 AND uses_count < max_uses AND expires_at > NOW()
RETURNING *;

-- name: ListActiveInvitesForConversation :many
SELECT * FROM conversation_invites
WHERE conversation_id = $1 AND uses_count < max_uses AND expires_at > NOW()
ORDER BY created_at DESC;

-- name: DeleteConversationInvite :exec
DELETE FROM conversation_invites
WHERE token = $1;
