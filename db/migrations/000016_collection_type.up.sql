-- Collections become single-type. `type` is a free TEXT (validated in app code, like
-- user roles) so a future 'exercises' type needs no schema change. Default 'cards'.
ALTER TABLE collections ADD COLUMN type TEXT NOT NULL DEFAULT 'cards';

-- Discard drafts that straddle the split: any draft that itself mixes cards and tests,
-- or whose published parent is mixed (publication is whole-collection, so these WIP
-- edits can't survive the split cleanly). Cascades remove their staged items.
DELETE FROM collections d
WHERE d.is_draft = true
  AND (
    (EXISTS (SELECT 1 FROM cards c WHERE c.collection_id = d.id)
       AND EXISTS (SELECT 1 FROM test_questions t WHERE t.collection_id = d.id))
    OR d.draft_of IN (
      SELECT p.id FROM collections p
      WHERE EXISTS (SELECT 1 FROM cards c2 WHERE c2.collection_id = p.id)
        AND EXISTS (SELECT 1 FROM test_questions t2 WHERE t2.collection_id = p.id)
    )
  );

-- Split mixed published collections: keep cards in the original (stays type 'cards'),
-- move test_questions into a new '<title> (tests)' collection of type 'tests'.
DO $$
DECLARE
  r record;
  new_id uuid;
BEGIN
  FOR r IN
    SELECT c.id, c.user_id, c.title, c.description, c.is_public
    FROM collections c
    WHERE c.is_draft = false
      AND EXISTS (SELECT 1 FROM cards cd WHERE cd.collection_id = c.id)
      AND EXISTS (SELECT 1 FROM test_questions t WHERE t.collection_id = c.id)
  LOOP
    INSERT INTO collections (user_id, title, description, is_public, type)
    VALUES (r.user_id, r.title || ' (tests)', r.description, r.is_public, 'tests')
    RETURNING id INTO new_id;

    UPDATE test_questions SET collection_id = new_id WHERE collection_id = r.id;
  END LOOP;
END $$;

-- Type any remaining single-content collection. After the split, anything that still
-- has test_questions and no cards is tests-only; cards-only and empty keep 'cards'.
UPDATE collections c SET type = 'tests'
WHERE c.type <> 'tests'
  AND EXISTS (SELECT 1 FROM test_questions t WHERE t.collection_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM cards cd WHERE cd.collection_id = c.id);
