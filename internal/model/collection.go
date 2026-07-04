package model

import "time"

type Collection struct {
	ID            string
	UserID        string
	Title         string
	Description   string
	IsPublic      bool
	DraftID       *string // non-nil for collections that have a pending item_draft overlay (not stored in DB)
	ShareToken    *string // non-nil when a share link has been generated
	Cards         []Card
	TestQuestions []TestQuestion
	Exercises     []Exercise
	Items         []Item // unified content (item-model)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
