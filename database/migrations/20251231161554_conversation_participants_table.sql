-- +goose Up
CREATE TABLE conversation_participants (
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Values: 'admin', 'member', 'observer'
    -- 'admin': Can add/remove users, post messages
    -- 'member': Can post messages (standard group and direct chats)
    -- 'observer': Read-only (standard channel subscriber)
    role VARCHAR(50) NOT NULL,
    last_read_at timestamptz NOT NULL DEFAULT (now()),
    PRIMARY KEY (conversation_id, user_id)
);

-- Fast lookup: "Show me all my chats"
CREATE INDEX idx_participants_user_id ON conversation_participants(user_id);

-- +goose Down
DROP TABLE IF EXISTS conversation_participants;
