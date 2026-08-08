// Package itemimport turns one entry of the unified import contract into a service.ImportItem.
//
// It exists so the JSON/YAML import endpoint and the MCP tools share one implementation. The
// invariants here — a quiz needs a correct option, an exercise sentence needs one answer per
// "___" blank, choice distractors come one list per blank — are exactly what an LLM gets wrong,
// and two copies of them would drift.
//
// Callers differ in policy, not in rules: the REST import skips bad entries and reports a count,
// the way the CSV import always skipped rows; the MCP tools return the error so the model can fix
// its output. Hence an error return rather than a bool.
package itemimport

import (
	"fmt"
	"strings"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

// BlankMarker is the placeholder for a gap in a sentence.
const BlankMarker = "___"

// Exercise kinds. "bank" sentences share one shuffled word pool, "choice" offers options per
// blank, "quiz" is a multiple-choice question (what used to be a test question).
const (
	KindBank   = "bank"
	KindChoice = "choice"
	KindQuiz   = "quiz"
)

// Option is one answer of a quiz.
type Option struct {
	Text        string
	Correct     bool
	Explanation string
}

// Sentence is one prompt of a bank/choice exercise, before validation.
type Sentence struct {
	Text        string
	Answer      []string
	Distractors [][]string // choice only: wrong options per blank
}

// Entry is one item of the unified import: the union of all shapes, with Type selecting which
// fields are read.
type Entry struct {
	Type string // card | quiz | exercise

	// card
	Term       string
	Definition string
	Hint       string

	// quiz
	Question string
	Options  []Option

	// exercise
	Kind        string
	Title       string
	Sentences   []Sentence
	Distractors []string // bank only: extra words for the shared pool
}

// BuildSentence validates one sentence against its exercise kind.
//
// The text must contain at least one "___" blank; Answer holds exactly one non-empty word per
// blank, in the order the blanks appear. For "choice", Distractors — when given — holds one list
// of wrong options per blank. For "bank" the pool is shared by the whole exercise, so per-sentence
// distractors are meaningless and ignored.
func BuildSentence(kind string, in Sentence) (model.ExerciseSentence, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return model.ExerciseSentence{}, fmt.Errorf("sentence text is empty")
	}
	nBlanks := strings.Count(text, BlankMarker)
	if nBlanks == 0 {
		return model.ExerciseSentence{}, fmt.Errorf("sentence %q has no %s blank", text, BlankMarker)
	}

	answer := make([]string, 0, len(in.Answer))
	for _, a := range in.Answer {
		answer = append(answer, strings.TrimSpace(a))
	}
	if len(answer) != nBlanks {
		return model.ExerciseSentence{}, fmt.Errorf(
			"sentence %q has %d blank(s) but %d answer(s); give exactly one answer per blank, in order",
			text, nBlanks, len(answer))
	}
	for i, a := range answer {
		if a == "" {
			return model.ExerciseSentence{}, fmt.Errorf("sentence %q: answer %d is empty", text, i+1)
		}
	}

	s := model.ExerciseSentence{Text: text, Answer: answer}
	if kind == KindChoice && len(in.Distractors) > 0 {
		if len(in.Distractors) != nBlanks {
			return model.ExerciseSentence{}, fmt.Errorf(
				"sentence %q has %d blank(s) but %d distractor list(s); for kind %q give one list of wrong options per blank",
				text, nBlanks, len(in.Distractors), KindChoice)
		}
		for _, gap := range in.Distractors {
			words := make([]string, 0, len(gap))
			for _, w := range gap {
				if w = strings.TrimSpace(w); w != "" {
					words = append(words, w)
				}
			}
			s.Distractors = append(s.Distractors, words)
		}
	}
	return s, nil
}

// BuildItem validates one entry and returns it ready for service.ImportItems.
func BuildItem(e Entry) (service.ImportItem, error) {
	switch strings.TrimSpace(e.Type) {
	case "card":
		// term/definition are canonical; question/answer are accepted as aliases because the
		// import contract has always taken them.
		term := firstNonEmpty(e.Term, e.Question)
		def := firstNonEmpty(e.Definition)
		if term == "" || def == "" {
			return service.ImportItem{}, fmt.Errorf("card needs both a term and a definition")
		}
		return service.ImportItem{Type: "card", Card: &model.Card{Term: term, Definition: def, Hint: strings.TrimSpace(e.Hint)}}, nil

	case "quiz":
		question := strings.TrimSpace(e.Question)
		if question == "" {
			return service.ImportItem{}, fmt.Errorf("quiz needs a question")
		}
		var opts []model.TestAnswer
		correct := 0
		for _, o := range e.Options {
			t := strings.TrimSpace(o.Text)
			if t == "" {
				continue
			}
			if o.Correct {
				correct++
			}
			opts = append(opts, model.TestAnswer{Text: t, IsCorrect: o.Correct, Explanation: strings.TrimSpace(o.Explanation)})
		}
		if len(opts) < 2 {
			return service.ImportItem{}, fmt.Errorf("quiz %q needs at least two options", question)
		}
		if correct == 0 {
			return service.ImportItem{}, fmt.Errorf("quiz %q has no option marked correct", question)
		}
		return service.ImportItem{Type: "quiz", Quiz: &model.TestQuestion{Question: question, Options: opts}}, nil

	case "exercise":
		kind := strings.TrimSpace(e.Kind)
		if kind != KindBank && kind != KindChoice {
			return service.ImportItem{}, fmt.Errorf(
				"exercise kind must be %q or %q, got %q (a multiple-choice question is type %q, not an exercise kind)",
				KindBank, KindChoice, kind, KindQuiz)
		}
		if len(e.Sentences) == 0 {
			return service.ImportItem{}, fmt.Errorf("exercise has no sentences")
		}
		ex := model.Exercise{Kind: kind, Title: strings.TrimSpace(e.Title)}
		if kind == KindBank {
			for _, d := range e.Distractors {
				if d = strings.TrimSpace(d); d != "" {
					ex.Distractors = append(ex.Distractors, d)
				}
			}
		}
		for i, in := range e.Sentences {
			s, err := BuildSentence(kind, in)
			if err != nil {
				return service.ImportItem{}, fmt.Errorf("sentence %d: %w", i+1, err)
			}
			ex.Sentences = append(ex.Sentences, s)
		}
		return service.ImportItem{Type: "exercise", Exercise: &ex}, nil

	default:
		return service.ImportItem{}, fmt.Errorf(`type must be "card", "quiz" or "exercise", got %q`, e.Type)
	}
}

// CountSentences reports how many sentences an entry carries, for callers enforcing a size cap.
func CountSentences(e Entry) int {
	if strings.TrimSpace(e.Type) != "exercise" {
		return 0
	}
	return len(e.Sentences)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
