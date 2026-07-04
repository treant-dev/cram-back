-- Partial rollback (greenfield): the dropped content/progress tables held data
-- that is not recoverable, so we only restore the structural columns on
-- collections and the cards FK. The old per-type tables are intentionally NOT
-- recreated — the item model is the source of truth after this migration.

ALTER TABLE collections ADD COLUMN IF NOT EXISTS type     TEXT NOT NULL DEFAULT 'cards';
ALTER TABLE collections ADD COLUMN IF NOT EXISTS is_draft BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE collections ADD COLUMN IF NOT EXISTS draft_of UUID REFERENCES collections(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_one_draft ON collections(draft_of) WHERE draft_of IS NOT NULL;

ALTER TABLE cards ADD CONSTRAINT cards_set_id_fkey
  FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE;
