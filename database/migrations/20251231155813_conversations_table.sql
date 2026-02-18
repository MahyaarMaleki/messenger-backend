-- +goose Up
CREATE TABLE conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- For private chats the other user's avatar url will be set
    name VARCHAR(100) NOT NULL,
    -- type: 'private' (1-on-1), 'group' (Many-to-Many), 'channel' (One-to-Many)
    type VARCHAR(20) NOT NULL DEFAULT 'private',
    -- For private chats the other user's avatar url will be set
    avatar_url VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS conversations;
