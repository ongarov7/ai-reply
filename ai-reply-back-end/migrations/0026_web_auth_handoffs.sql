-- Short-lived one-use capability; never persist its raw URL/code or app tokens.
CREATE TABLE web_auth_handoffs (
    code_hash   TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    consumed_at INTEGER
);
CREATE INDEX web_auth_handoffs_expiry ON web_auth_handoffs(expires_at);
