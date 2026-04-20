CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    google_id  TEXT NOT NULL UNIQUE,
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    picture    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE study_sets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_study_sets_user_id ON study_sets(user_id);

CREATE TABLE cards (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id     UUID NOT NULL REFERENCES study_sets(id) ON DELETE CASCADE,
    question   TEXT NOT NULL,
    answer     TEXT NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cards_set_id ON cards(set_id);

CREATE TABLE test_questions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id     UUID NOT NULL REFERENCES study_sets(id) ON DELETE CASCADE,
    question   TEXT NOT NULL,
    options    JSONB NOT NULL DEFAULT '[]',
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_test_questions_set_id ON test_questions(set_id);
