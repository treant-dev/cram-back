package model

import "time"

type Card struct {
	ID           string
	CollectionID string
	Term         string
	Definition   string
	Image        string
	Position     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
