ALTER TABLE study_sets RENAME TO collections;
ALTER TABLE collections ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT false;

ALTER INDEX idx_study_sets_user_id RENAME TO idx_collections_user_id;

ALTER TABLE cards RENAME COLUMN set_id TO collection_id;
ALTER INDEX idx_cards_set_id RENAME TO idx_cards_collection_id;

ALTER TABLE test_questions RENAME COLUMN set_id TO collection_id;
ALTER INDEX idx_test_questions_set_id RENAME TO idx_test_questions_collection_id;
