-- 0017_support_chat: қолданушы мен қолдау операторы арасындағы чат.
--
-- One conversation per account (UNIQUE user_id), opened by the person's first
-- message. The person writes from the app (/api/v1/support/*); operators answer
-- from the admin panel (/api/v1/admin/support/*). The unread counters are kept
-- on the conversation so the app badge and the admin list never count messages.
-- client_message_id makes the app's retries idempotent per conversation.
--
-- Retention: messages are kept until the account is deleted; then the
-- conversation and every message go with it (ON DELETE CASCADE). An operator
-- that is removed leaves the messages, without the link to the admin.
--
-- Only additive. Rollback: older binaries do not know the tables and leave them
-- alone; drop them by hand afterwards (support_messages first).
CREATE TABLE support_conversations (
    id                    TEXT PRIMARY KEY,
    user_id               TEXT NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    status                TEXT NOT NULL DEFAULT 'open',      -- open | resolved
    assigned_admin_id     TEXT REFERENCES admin_users (id) ON DELETE SET NULL,
    last_message_at       INTEGER NOT NULL,
    user_unread_count     INTEGER NOT NULL DEFAULT 0,        -- operator messages the person has not read
    operator_unread_count INTEGER NOT NULL DEFAULT 0,        -- the person's messages no operator has read
    resolved_at           INTEGER,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);
CREATE INDEX idx_support_conversations_status ON support_conversations (status, last_message_at);
CREATE INDEX idx_support_conversations_unread ON support_conversations (operator_unread_count, last_message_at);

CREATE TABLE support_messages (
    id                TEXT PRIMARY KEY,
    conversation_id   TEXT NOT NULL REFERENCES support_conversations (id) ON DELETE CASCADE,
    sender            TEXT NOT NULL,                         -- user | operator
    admin_id          TEXT REFERENCES admin_users (id) ON DELETE SET NULL,
    client_message_id TEXT,                                  -- the app's UUID for retries; NULL for operators
    body              TEXT NOT NULL,                         -- 1-2000 characters
    created_at        INTEGER NOT NULL,                      -- strictly increasing within a conversation
    read_at           INTEGER,                               -- read by the other side
    UNIQUE (conversation_id, client_message_id)
);
CREATE INDEX idx_support_messages_conversation ON support_messages (conversation_id, created_at, id);
