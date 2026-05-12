package model

import "time"

type TestAnswer struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	IsCorrect   bool   `json:"is_correct"`
	Explanation string `json:"explanation"`
	Position    int    `json:"position"`
}

type TestQuestion struct {
	ID           string
	CollectionID string
	Question     string
	Options      []TestAnswer
	Image        string
	Position     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
