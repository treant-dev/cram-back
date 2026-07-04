package model

import "time"

// Item is the unified content row. Cards, tests, exercises and sentences all live
// in `items`, distinguished by Type, with type-specific fields in Content (JSONB).
//
// Content shapes by Type (validated in app code, not by columns):
//
//	card     : {"term","definition","image"?}
//	test     : {"question","options":[{"text","is_correct","explanation"?}],"image"?}
//	exercise : {"kind":"bank"|"choice","title","distractors":[...]}
//	sentence : {"text","answer":[...],"distractors":[...]?,"hint"?}   (ParentID → exercise)
type Item struct {
	ID           string
	Type         string
	CollectionID *string        // nil = free-floating
	ParentID     *string        // structural parent (e.g. sentence → exercise)
	Content      map[string]any // type-specific fields (JSONB)
	Rank         string         // fractional index (see internal/rank)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ItemDraft is a staged, not-yet-published edit. Shares ItemID with the live item
// for upsert/delete; a new ItemID for additions. Op is "upsert" | "delete".
type ItemDraft struct {
	ItemID       string
	CollectionID string
	Op           string
	Type         *string
	ParentID     *string
	Content      map[string]any
	Rank         *string
	UpdatedAt    time.Time
}

// ItemProgress is spaced-repetition state — ONLY for cards (driver: blitz).
type ItemProgress struct {
	UserID       string
	ItemID       string
	Level        int
	NextReviewAt time.Time
	LastReviewAt *time.Time
}

// ItemEvent is one append-only attempt record. Correct == nil marks a retake reset.
type ItemEvent struct {
	ID        string
	UserID    string
	ItemID    string
	Correct   *bool
	Payload   map[string]any
	CreatedAt time.Time
}
