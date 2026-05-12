ALTER TABLE card_progress RENAME TO user_card_progress;
ALTER TABLE tq_progress RENAME TO user_test_progress;
ALTER TABLE user_tq_events RENAME TO user_test_events;

DROP TABLE IF EXISTS user_card_stats;
DROP TABLE IF EXISTS user_tq_stats;
