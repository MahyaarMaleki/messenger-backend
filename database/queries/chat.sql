-- name: CreateConversation :one
INSERT INTO conversations (
    name,
    type
) VALUES (
    $1, $2
) RETURNING *;

-- name: AddParticipant :one
INSERT INTO conversation_participants (
    conversation_id,
    user_id,
    role
) VALUES (
    $1, $2, $3
) RETURNING *;

-- name: CreateMessage :one
INSERT INTO messages (
    conversation_id,
    sender_id,
    content
) VALUES (
    $1, $2, $3
) RETURNING *;

-- name: GetConversation :one
SELECT * FROM conversations
WHERE id = $1 LIMIT 1;

-- name: GetConversationMessages :many
-- GetConversationMessages Loads messages for a specific chat with pagination support
SELECT
    id,
    conversation_id,
    sender_id,
    content,
    created_at,
    updated_at
FROM messages
WHERE conversation_id = $1
ORDER BY created_at DESC
LIMIT $2
OFFSET $3;

-- name: GetUserConversations :many
-- GetUserConversations lists all the conversations a user is part of
SELECT
    c.id,
    c.name,
    c.type,
    c.created_at,
    c.last_message_at,
    cp.role,
    cp.joined_at
FROM conversations c
JOIN conversation_participants cp ON c.id = cp.conversation_id
WHERE cp.user_id = $1
ORDER BY c.last_message_at DESC;

-- name: GetParticipant :one
SELECT * FROM conversation_participants
WHERE conversation_id = $1 AND user_id = $2
LIMIT 1;

-- name: FindExistingPrivateChat :one
SELECT c.id
FROM conversations c
JOIN conversation_participants cp1 ON c.id = cp1.conversation_id
JOIN conversation_participants cp2 ON c.id = cp2.conversation_id
WHERE c.type = 'private'
    AND cp1.user_id = $1
    AND cp2.user_id = $2
LIMIT 1;

-- name: UpdateConversationLastMessageAt :exec
UPDATE conversations
SET last_message_at = $2
WHERE id = $1;
