DROP INDEX IF EXISTS idx_collections_one_draft;
ALTER TABLE collections DROP COLUMN draft_of;
ALTER TABLE collections DROP COLUMN is_draft;
