ALTER INDEX idx_test_questions_collection_id RENAME TO idx_test_questions_set_id;
ALTER TABLE test_questions RENAME COLUMN collection_id TO set_id;

ALTER INDEX idx_cards_collection_id RENAME TO idx_cards_set_id;
ALTER TABLE cards RENAME COLUMN collection_id TO set_id;

ALTER INDEX idx_collections_user_id RENAME TO idx_study_sets_user_id;
ALTER TABLE collections DROP COLUMN is_public;
ALTER TABLE collections RENAME TO study_sets;
