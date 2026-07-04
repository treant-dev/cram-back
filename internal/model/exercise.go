package model

import "time"

// ExerciseSentence is one prompt within an exercise. Text contains one or more "___"
// blanks; Answer holds the correct word for each blank, in order. For "choice" exercises
// Distractors holds the wrong option words per blank (Distractors[i] = wrong words for
// blank i; the correct one is Answer[i]).
type ExerciseSentence struct {
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	Answer      []string   `json:"answer"`                // one word per blank, in order
	Distractors [][]string `json:"distractors,omitempty"` // choice only: wrong options per blank
	Position    int        `json:"position"`
}

// Exercise is a group of fill-in-the-blank sentences of one kind.
//
//	"bank"   — sentences share one shuffled word pool (matching); Distractors adds extra pool words.
//	"choice" — each sentence offers Answer + per-sentence Distractors to pick between.
type Exercise struct {
	ID           string             `json:"ID"`
	CollectionID string             `json:"CollectionID"`
	Kind         string             `json:"Kind"` // "bank" | "choice" | "quiz"
	Title        string             `json:"Title"`
	Sentences    []ExerciseSentence `json:"Sentences"`
	Distractors  []string           `json:"Distractors"` // bank only: extra words for the shared pool
	// quiz only: a multiple-choice question (a "test" is now a quiz exercise).
	Question string       `json:"Question,omitempty"`
	Options  []TestAnswer `json:"Options,omitempty"`
	Position  int         `json:"Position"`
	CreatedAt time.Time   `json:"CreatedAt"`
	UpdatedAt time.Time   `json:"UpdatedAt"`
}
