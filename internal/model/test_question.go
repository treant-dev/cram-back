package model

import "time"

type TestAnswer struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	IsCorrect   bool   `json:"is_correct"`
	Explanation string `json:"explanation"`
	Position    int    `json:"position"`
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
	Options      []TestAnswer
	Image        string
	Position     int
	Stats        *TQStats `json:"Stats,omitempty"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
