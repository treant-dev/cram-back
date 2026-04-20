package model

import "time"

type CardStats struct {
	Correct   int        `json:"Correct"`
	Incorrect int        `json:"Incorrect"`
	Streak    int        `json:"Streak"`
	LastSeen  *time.Time `json:"LastSeen,omitempty"`
}

type Card struct {
	ID           string
	CollectionID string
	Question     string
	Answer       string
	Position     int
	Stats        *CardStats `json:"Stats,omitempty"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
