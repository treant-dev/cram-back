ALTER TABLE cards ADD COLUMN source_card_id UUID REFERENCES cards(id) ON DELETE SET NULL;
ALTER TABLE test_questions ADD COLUMN source_tq_id UUID REFERENCES test_questions(id) ON DELETE SET NULL;
