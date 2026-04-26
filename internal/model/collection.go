package model

import "time"

type Collection struct {
	ID            string
	UserID        string
	Title         string
	Description   string
	IsPublic      bool
	IsDraft       bool
	DraftOf       *string // non-nil for draft collections
	DraftID       *string // non-nil for active collections that have a pending draft (not stored in DB)
	ShareToken    *string // non-nil when a share link has been generated
	Cards         []Card
	TestQuestions []TestQuestion
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
