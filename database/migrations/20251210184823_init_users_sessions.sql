-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username varchar(30) NOT NULL UNIQUE,
    email varchar(255) NOT NULL UNIQUE,
    password_hash varchar NOT NULL,

    -- Profile Information
    first_name varchar(50) NOT NULL,
    last_name varchar(50) NOT NULL,
    bio varchar(70),
    birthday date,
    avatar_url varchar,

    created_at timestamptz NOT NULL DEFAULT (now()),
    updated_at timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL,
    refresh_token varchar NOT NULL,
    user_agent varchar(255) NOT NULL, -- To show "Logged in on Chrome/Windows"
    client_ip varchar(45) NOT NULL,  -- For security alerts
    is_blocked boolean NOT NULL DEFAULT false, -- To revoke a specific device
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT (now())
);

-- Foreign Key: If user is deleted, delete their sessions
ALTER TABLE sessions ADD FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;

-- Indexes for performance
CREATE INDEX ON users (username);
CREATE INDEX ON users (email);
CREATE INDEX ON sessions (user_id);

-- +goose Down
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_user_id_fkey;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
