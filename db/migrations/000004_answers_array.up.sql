ALTER TABLE cards ADD COLUMN answers TEXT[] NOT NULL DEFAULT '{}';
UPDATE cards SET answers = ARRAY[answer];
ALTER TABLE cards DROP COLUMN answer;
