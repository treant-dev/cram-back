package model

import "time"

type User struct {
	ID        string
	GoogleID  string
	Email     string
	Name      string
	Picture   string
	CreatedAt time.Time
	UpdatedAt time.Time
}
