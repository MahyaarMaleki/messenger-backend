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

-- name: CreateAttachment :exec
INSERT INTO message_attachments (
    message_id,
    file_url,
    file_type,
    file_name
) VALUES (
    $1, $2, $3, $4
);

-- name: GetConversationMessages :many
-- GetConversationMessages Loads messages for a specific chat with pagination support
SELECT
    m.id,
    m.conversation_id,
    m.sender_id,
    m.content,
    m.created_at,
    m.updated_at,
    COALESCE(
        json_agg(
        json_build_object(
            'id', ma.id,
            'url', ma.file_url,
            'type', ma.file_type,
            'name', ma.file_name
        )
    ) FILTER (WHERE ma.id IS NOT NULL),
        '[]'
    )::jsonb AS attachments
FROM messages m
LEFT JOIN message_attachments ma ON m.id = ma.message_id
WHERE m.conversation_id = $1
GROUP BY m.id, m.created_at
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetUserConversations :many
-- GetUserConversations lists all the conversations a user is part of
SELECT
    c.id,
    c.name,
    c.type,
    c.created_at,
    c.last_message_at,
    cp.role,
    cp.joined_at,
    -- Fetch the "Other User" info (Nullable, only for private chats)
    u.username AS other_username,
    u.first_name AS other_first_name,
    u.last_name AS other_last_name,
    u.bio AS other_bio,
    u.avatar_url AS other_avatar_url
FROM conversations c
JOIN conversation_participants cp ON c.id = cp.conversation_id
-- Logic: If type is 'private', find the participant who is NOT me ($1)
LEFT JOIN conversation_participants cp2
    ON c.id = cp2.conversation_id
    AND cp2.user_id != $1
    AND c.type = 'private'
LEFT JOIN users u ON cp2.user_id = u.id
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

-- name: GetMessage :one
SELECT * FROM messages
WHERE id = $1 LIMIT 1;

-- name: UpdateMessage :one
UPDATE messages
SET content = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMessage :exec
DELETE FROM messages
WHERE id = $1;

-- name: RemoveParticipant :exec
DELETE FROM conversation_participants
WHERE conversation_id = $1 AND user_id = $2;

-- name: UpdateConversation :one
UPDATE conversations
SET name = $2
WHERE id = $1
RETURNING *;

-- name: ListParticipants :many
SELECT
    u.username,
    u.avatar_url,
    cp.user_id,
    cp.role,
    cp.joined_at
FROM conversation_participants cp
JOIN users u ON cp.user_id = u.id
WHERE cp.conversation_id = $1
ORDER BY cp.role, u.username;