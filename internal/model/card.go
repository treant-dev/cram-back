package model

import "time"

type Card struct {
	ID           string
	CollectionID string
	Term         string
	Definition   string
	// Hint is optional guidance shown on request while studying — a mnemonic, a warning about an
	// irregular form. Deliberately separate from Definition, which is the answer itself.
	Hint      string
	Image     string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}
