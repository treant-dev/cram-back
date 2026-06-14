package handler

import (
	"reflect"
	"testing"
)

func TestBuildSentence(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		in          yamlSentence
		wantOK      bool
		wantAnswer  []string
		wantDistrac [][]string
	}{
		{
			name:       "bank single blank",
			kind:       "bank",
			in:         yamlSentence{Text: "How ___ you?", Answer: []string{"are"}},
			wantOK:     true,
			wantAnswer: []string{"are"},
		},
		{
			name:       "bank multi blank",
			kind:       "bank",
			in:         yamlSentence{Text: "My ___ ___ Vasiliy", Answer: []string{"name", "is"}},
			wantOK:     true,
			wantAnswer: []string{"name", "is"},
		},
		{
			name:   "answer count must match blank count",
			kind:   "bank",
			in:     yamlSentence{Text: "How ___ you?", Answer: []string{"are", "extra"}},
			wantOK: false,
		},
		{
			name:   "no blank is invalid",
			kind:   "bank",
			in:     yamlSentence{Text: "Hello there", Answer: nil},
			wantOK: false,
		},
		{
			name:   "empty answer word is invalid",
			kind:   "bank",
			in:     yamlSentence{Text: "How ___ you?", Answer: []string{"  "}},
			wantOK: false,
		},
		{
			name:        "choice single blank with per-gap distractors",
			kind:        "choice",
			in:          yamlSentence{Text: "Wir ___ Brot", Answer: []string{"essen"}, Distractors: [][]string{{"esst", "trinken"}}},
			wantOK:      true,
			wantAnswer:  []string{"essen"},
			wantDistrac: [][]string{{"esst", "trinken"}},
		},
		{
			name:        "choice multi blank — one distractor list per gap",
			kind:        "choice",
			in:          yamlSentence{Text: "Sie ___ nach ___", Answer: []string{"geht", "Hause"}, Distractors: [][]string{{"gehst"}, {"Schule", "Stadt"}}},
			wantOK:      true,
			wantAnswer:  []string{"geht", "Hause"},
			wantDistrac: [][]string{{"gehst"}, {"Schule", "Stadt"}},
		},
		{
			name:   "choice distractors must have one list per blank",
			kind:   "choice",
			in:     yamlSentence{Text: "Sie ___ nach ___", Answer: []string{"geht", "Hause"}, Distractors: [][]string{{"gehst"}}},
			wantOK: false,
		},
		{
			name:       "text is trimmed",
			kind:       "bank",
			in:         yamlSentence{Text: "  How ___ you?  ", Answer: []string{"are"}},
			wantOK:     true,
			wantAnswer: []string{"are"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := buildSentence(tc.kind, tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if !reflect.DeepEqual(got.Answer, tc.wantAnswer) {
				t.Errorf("answer = %v, want %v", got.Answer, tc.wantAnswer)
			}
			if tc.wantDistrac != nil && !reflect.DeepEqual(got.Distractors, tc.wantDistrac) {
				t.Errorf("distractors = %v, want %v", got.Distractors, tc.wantDistrac)
			}
			if got.Text != "How ___ you?" && tc.name == "text is trimmed" {
				t.Errorf("text = %q, want trimmed", got.Text)
			}
		})
	}
}
