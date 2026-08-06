-- Long-lived bearer tokens for non-browser clients (MCP server). Only the SHA-256 hash of
-- the token is stored; the plaintext is shown once at creation and never again.
CREATE TABLE personal_access_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,             -- hex-encoded SHA-256 of the full token string
    scope        TEXT NOT NULL,                    -- 'read' | 'read_write'
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,                      -- NULL = never expires
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX idx_pat_user ON personal_access_tokens(user_id);
