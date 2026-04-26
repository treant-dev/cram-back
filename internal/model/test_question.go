package model

import "time"

type TestOption struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
}

type TQStats struct {
	Correct   int        `json:"Correct"`
	Incorrect int        `json:"Incorrect"`
	Streak    int        `json:"Streak"`
	LastSeen  *time.Time `json:"LastSeen,omitempty"`
}

type TestQuestion struct {
	ID           string
	CollectionID string
	Question     string
	Options      []TestOption
	Image        string
	Position     int
	Stats        *TQStats `json:"Stats,omitempty"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
