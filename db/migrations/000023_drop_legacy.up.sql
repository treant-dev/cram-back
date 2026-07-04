-- Cutover to the unified item model. The Go code no longer reads or writes the
-- legacy content/progress tables — everything lives in items/item_draft/
-- item_progress/item_events/item_history now.
--
-- Greenfield: data is NOT migrated. `cards` is kept as a frozen restore point
-- (detached from collections); everything else is dropped.

-- progress / events — not carried over
DROP TABLE IF EXISTS user_sentence_answers;
DROP TABLE IF EXISTS user_sentence_progress;
DROP TABLE IF EXISTS user_card_events;
DROP TABLE IF EXISTS user_test_events;
DROP TABLE IF EXISTS user_card_progress;
DROP TABLE IF EXISTS user_test_progress;

-- old content, EXCEPT cards
DROP TABLE IF EXISTS exercise_sentences;
DROP TABLE IF EXISTS exercises;
DROP TABLE IF EXISTS test_answers;
DROP TABLE IF EXISTS test_questions;

-- collections: drop single-type + draft machinery (now item_draft-based)
DROP INDEX IF EXISTS idx_collections_one_draft;
ALTER TABLE collections DROP COLUMN IF EXISTS type;
ALTER TABLE collections DROP COLUMN IF EXISTS is_draft;
ALTER TABLE collections DROP COLUMN IF EXISTS draft_of;

-- cards: freeze standalone — detach the FK to collections so cascade deletes
-- can't touch the restore point. (Try both known constraint names.)
ALTER TABLE cards DROP CONSTRAINT IF EXISTS cards_set_id_fkey;
ALTER TABLE cards DROP CONSTRAINT IF EXISTS cards_collection_id_fkey;
