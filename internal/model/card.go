package model

import "time"

type Card struct {
	ID        string
	SetID     string
	Question  string
	Answer    string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}
