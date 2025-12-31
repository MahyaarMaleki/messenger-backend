-- +goose Up
CREATE TABLE conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NULL for 1-on-1 chats, Set for groups and channels
    name VARCHAR(100),

    -- type: 'private' (1-on-1), 'group' (Many-to-Many), 'channel' (One-to-Many)
    type VARCHAR(20) NOT NULL DEFAULT 'private',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS conversations;
