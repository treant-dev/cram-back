package itemimport

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildSentence(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		in          Sentence
		wantOK      bool
		wantAnswer  []string
		wantDistrac [][]string
	}{
		{"bank single blank", KindBank, Sentence{Text: "How ___ you?", Answer: []string{"are"}}, true, []string{"are"}, nil},
		{"bank multi blank", KindBank, Sentence{Text: "My ___ ___ Vasiliy", Answer: []string{"name", "is"}}, true, []string{"name", "is"}, nil},
		{"answer count must match blank count", KindBank, Sentence{Text: "How ___ you?", Answer: []string{"are", "extra"}}, false, nil, nil},
		{"fewer answers than blanks", KindBank, Sentence{Text: "___ and ___", Answer: []string{"a"}}, false, nil, nil},
		{"no blank is invalid", KindBank, Sentence{Text: "Hello there", Answer: nil}, false, nil, nil},
		{"empty answer word is invalid", KindBank, Sentence{Text: "How ___ you?", Answer: []string{"  "}}, false, nil, nil},
		{"empty text is invalid", KindBank, Sentence{Text: "   ", Answer: []string{"a"}}, false, nil, nil},
		{
			"choice with per-gap distractors", KindChoice,
			Sentence{Text: "Wir ___ Brot", Answer: []string{"essen"}, Distractors: [][]string{{"esst", "trinken"}}},
			true, []string{"essen"}, [][]string{{"esst", "trinken"}},
		},
		{
			"choice needs one distractor list per blank", KindChoice,
			Sentence{Text: "Sie ___ nach ___", Answer: []string{"geht", "Hause"}, Distractors: [][]string{{"gehst"}}},
			false, nil, nil,
		},
		{
			"bank ignores per-sentence distractors", KindBank,
			Sentence{Text: "How ___ you?", Answer: []string{"are"}, Distractors: [][]string{{"is"}}},
			true, []string{"are"}, nil,
		},
		{"text is trimmed", KindBank, Sentence{Text: "  How ___ you?  ", Answer: []string{"are"}}, true, []string{"are"}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildSentence(tc.kind, tc.in)
			if (err == nil) != tc.wantOK {
				t.Fatalf("err = %v, wantOK %v", err, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if !reflect.DeepEqual(got.Answer, tc.wantAnswer) {
				t.Errorf("answer = %v, want %v", got.Answer, tc.wantAnswer)
			}
			if !reflect.DeepEqual(got.Distractors, tc.wantDistrac) {
				t.Errorf("distractors = %v, want %v", got.Distractors, tc.wantDistrac)
			}
			if strings.HasPrefix(got.Text, " ") || strings.HasSuffix(got.Text, " ") {
				t.Errorf("text = %q, want trimmed", got.Text)
			}
		})
	}
}

// Error text is the contract for MCP callers: a model has to be able to fix its output from the
// message alone, so the mistakes it actually makes must be named precisely.
func TestSentenceErrorsAreActionable(t *testing.T) {
	_, err := BuildSentence(KindBank, Sentence{Text: "How ___ you?", Answer: []string{"are", "extra"}})
	if err == nil || !strings.Contains(err.Error(), "1 blank(s) but 2 answer(s)") {
		t.Errorf("blank/answer mismatch not explained: %v", err)
	}

	_, err = BuildSentence(KindChoice, Sentence{
		Text: "Sie ___ nach ___", Answer: []string{"geht", "Hause"}, Distractors: [][]string{{"gehst"}},
	})
	if err == nil || !strings.Contains(err.Error(), "distractor list(s)") {
		t.Errorf("distractor shape not explained: %v", err)
	}
}

func TestBuildItemCard(t *testing.T) {
	got, err := BuildItem(Entry{Type: "card", Term: "  hola  ", Definition: "hello"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if got.Type != "card" || got.Card == nil || got.Card.Term != "hola" {
		t.Errorf("unexpected card: %+v", got.Card)
	}

	// question is a legacy alias for the card front; the import contract has always taken it.
	got, err = BuildItem(Entry{Type: "card", Question: "adios", Definition: "goodbye"})
	if err != nil || got.Card.Term != "adios" {
		t.Errorf("question alias not honoured: %+v, %v", got.Card, err)
	}

	for _, e := range []Entry{
		{Type: "card", Term: "", Definition: "x"},
		{Type: "card", Term: "x", Definition: "  "},
	} {
		if _, err := BuildItem(e); err == nil {
			t.Errorf("half-empty card accepted: %+v", e)
		}
	}
}

func TestBuildItemQuiz(t *testing.T) {
	ok := Entry{Type: "quiz", Question: "Capital?", Options: []Option{
		{Text: "Madrid", Correct: true}, {Text: "Lisbon"},
	}}
	got, err := BuildItem(ok)
	if err != nil {
		t.Fatalf("quiz: %v", err)
	}
	if len(got.Quiz.Options) != 2 || !got.Quiz.Options[0].IsCorrect {
		t.Errorf("unexpected options: %+v", got.Quiz.Options)
	}

	t.Run("no correct option", func(t *testing.T) {
		e := Entry{Type: "quiz", Question: "q", Options: []Option{{Text: "a"}, {Text: "b"}}}
		if _, err := BuildItem(e); err == nil || !strings.Contains(err.Error(), "no option marked correct") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("single option", func(t *testing.T) {
		e := Entry{Type: "quiz", Question: "q", Options: []Option{{Text: "a", Correct: true}}}
		if _, err := BuildItem(e); err == nil || !strings.Contains(err.Error(), "at least two options") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("blank options are dropped, not counted", func(t *testing.T) {
		e := Entry{Type: "quiz", Question: "q", Options: []Option{{Text: "a", Correct: true}, {Text: "  "}}}
		if _, err := BuildItem(e); err == nil {
			t.Error("a quiz left with one real option was accepted")
		}
	})
}

func TestBuildItemExercise(t *testing.T) {
	t.Run("bank keeps the pool and trims blanks", func(t *testing.T) {
		got, err := BuildItem(Entry{
			Type: "exercise", Kind: KindBank, Title: "  Greetings  ",
			Distractors: []string{"tschüss", "  ", "hallo"},
			Sentences:   []Sentence{{Text: "How ___ you?", Answer: []string{"are"}}},
		})
		if err != nil {
			t.Fatalf("bank: %v", err)
		}
		if got.Exercise.Title != "Greetings" {
			t.Errorf("title = %q, want trimmed", got.Exercise.Title)
		}
		if len(got.Exercise.Distractors) != 2 {
			t.Errorf("pool = %v, want blanks dropped", got.Exercise.Distractors)
		}
	})

	t.Run("choice drops the shared pool", func(t *testing.T) {
		got, err := BuildItem(Entry{
			Type: "exercise", Kind: KindChoice, Distractors: []string{"ignored"},
			Sentences: []Sentence{{Text: "Wir ___ Brot", Answer: []string{"essen"}}},
		})
		if err != nil {
			t.Fatalf("choice: %v", err)
		}
		if got.Exercise.Distractors != nil {
			t.Errorf("choice kept a shared pool: %v", got.Exercise.Distractors)
		}
	})

	t.Run("quiz is a type, not an exercise kind", func(t *testing.T) {
		// The likeliest confusion now that a quiz is stored as an exercise: the error has to
		// point at the right field rather than just saying "invalid".
		_, err := BuildItem(Entry{Type: "exercise", Kind: KindQuiz, Sentences: []Sentence{{Text: "a ___", Answer: []string{"b"}}}})
		if err == nil || !strings.Contains(err.Error(), `type "quiz"`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("no sentences", func(t *testing.T) {
		if _, err := BuildItem(Entry{Type: "exercise", Kind: KindBank}); err == nil {
			t.Error("exercise without sentences accepted")
		}
	})

	t.Run("names the offending sentence", func(t *testing.T) {
		_, err := BuildItem(Entry{Type: "exercise", Kind: KindBank, Sentences: []Sentence{
			{Text: "ok ___", Answer: []string{"a"}},
			{Text: "no blanks", Answer: nil},
		}})
		if err == nil || !strings.Contains(err.Error(), "sentence 2") {
			t.Errorf("error does not point at the bad sentence: %v", err)
		}
	})
}

func TestBuildItemUnknownType(t *testing.T) {
	if _, err := BuildItem(Entry{Type: "flashcard"}); err == nil || !strings.Contains(err.Error(), "flashcard") {
		t.Errorf("unknown type not reported clearly: %v", err)
	}
}

func TestCountSentences(t *testing.T) {
	e := Entry{Type: "exercise", Kind: KindBank, Sentences: []Sentence{{}, {}}}
	if CountSentences(e) != 2 {
		t.Errorf("exercise: got %d, want 2", CountSentences(e))
	}
	if CountSentences(Entry{Type: "card"}) != 0 {
		t.Error("card should count no sentences")
	}
}

func TestCardHint(t *testing.T) {
	got, err := BuildItem(Entry{Type: "card", Term: "der Löffel", Definition: "spoon", Hint: "  masculine  "})
	if err != nil {
		t.Fatalf("card with hint: %v", err)
	}
	if got.Card.Hint != "masculine" {
		t.Errorf("hint = %q, want it trimmed", got.Card.Hint)
	}

	// A hint is optional; a card without one is still a card.
	got, err = BuildItem(Entry{Type: "card", Term: "a", Definition: "b"})
	if err != nil || got.Card.Hint != "" {
		t.Errorf("card without a hint: %+v, %v", got.Card, err)
	}
}
