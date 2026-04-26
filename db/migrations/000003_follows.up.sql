CREATE TABLE collection_follows (
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    collection_id UUID NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    followed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, collection_id)
);

CREATE INDEX idx_collection_follows_collection_id ON collection_follows(collection_id);
