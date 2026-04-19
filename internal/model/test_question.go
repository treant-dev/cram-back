package model

import "time"

type TestOption struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
}

type TestQuestion struct {
	ID        string
	SetID     string
	Question  string
	Options   []TestOption
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}
