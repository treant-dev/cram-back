ALTER TABLE collections ADD COLUMN is_draft BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE collections ADD COLUMN draft_of UUID REFERENCES collections(id) ON DELETE CASCADE;

-- Enforce at most one draft per collection
CREATE UNIQUE INDEX idx_collections_one_draft ON collections(draft_of) WHERE draft_of IS NOT NULL;
