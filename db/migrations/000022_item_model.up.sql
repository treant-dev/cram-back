-- Item-модель: единый контент (items) + черновик (item_draft) + прогресс/события
-- (item_progress/item_events) + история (item_history). Аддитивно — старые таблицы
-- живут; их дроп в 000023 (в момент cutover кода). users/collections уже существуют.

-- ── items: реестр всего контента ───────────────────────────────────────────
CREATE TABLE items (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type          TEXT NOT NULL,                                    -- 'card'|'test'|'exercise'|'sentence'|...
    collection_id UUID REFERENCES collections(id) ON DELETE CASCADE,
    parent_id     UUID REFERENCES items(id) ON DELETE CASCADE,      -- структурный родитель (напр. sentence → exercise)
    content       JSONB NOT NULL DEFAULT '{}'::jsonb,
    rank          TEXT NOT NULL,                                    -- fractional indexing (LexoRank-style)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_items_collection  ON items(collection_id);
CREATE INDEX idx_items_parent      ON items(parent_id);
CREATE INDEX idx_items_content_gin ON items USING GIN (content);

-- ── item_draft: изолированный черновик правок (ворота) ──────────────────────
CREATE TABLE item_draft (
    item_id       UUID PRIMARY KEY,                                 -- тот же id, что у live item; новый = добавлен в черновике
    collection_id UUID NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    op            TEXT NOT NULL,                                    -- 'upsert' | 'delete'
    type          TEXT,
    parent_id     UUID,
    content       JSONB,
    rank          TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_item_draft_collection ON item_draft(collection_id);

-- ── item_progress: spaced-repetition, ТОЛЬКО карточки (драйвер — blitz) ──────
CREATE TABLE item_progress (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id        UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    level          INT  NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 7),
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_review_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX idx_item_progress_due ON item_progress(user_id, next_review_at);

-- ── item_events: append-only лог попыток (упражнения: запись + показ) ────────
CREATE TABLE item_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    correct    BOOLEAN,                                             -- NULL = маркер сброса (retake)
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,                  -- напр. {"submitted":[...]}
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_item_events_user_item ON item_events(user_id, item_id, created_at DESC);

-- ── item_history: before-image лог опубликованного (откат/timewalk) ─────────
CREATE TABLE item_history (
    seq           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id       UUID NOT NULL,
    collection_id UUID,                                             -- без FK: триггер логирует и во время cascade-delete коллекции (FK бы упал на удаляемой коллекции)
    snapshot      JSONB,                                            -- вся прежняя строка items; NULL = был insert
    created_at    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX idx_item_history_timeline ON item_history(collection_id, seq DESC);
CREATE INDEX idx_item_history_item     ON item_history(item_id, seq DESC);

CREATE OR REPLACE FUNCTION log_item_history() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO item_history (item_id, collection_id, snapshot)
        VALUES (NEW.id, NEW.collection_id, NULL);
    ELSE  -- UPDATE / DELETE: before-образ
        INSERT INTO item_history (item_id, collection_id, snapshot)
        VALUES (OLD.id, OLD.collection_id, to_jsonb(OLD));
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_item_history
    AFTER INSERT OR UPDATE OR DELETE ON items
    FOR EACH ROW EXECUTE FUNCTION log_item_history();
