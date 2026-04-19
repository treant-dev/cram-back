ALTER TABLE cards ADD COLUMN answer TEXT NOT NULL DEFAULT '';
UPDATE cards SET answer = answers[1];
ALTER TABLE cards DROP COLUMN answers;
