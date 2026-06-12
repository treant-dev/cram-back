-- Note: this only drops the column. The data split (test_questions moved into separate
-- collections) is not reversed — merging them back is not attempted.
ALTER TABLE collections DROP COLUMN type;
