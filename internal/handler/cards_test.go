package handler

import "testing"

func TestLooksStructured(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"hund;dog\nkatze;cat", false}, // legacy CSV shape — no longer imported
		{`[{"question":"a","answer":"b"}]`, true},
		{"  \n[1,2]", true},                      // leading whitespace
		{"- question: a\n  answer: b", true},      // yaml block list
		{"---\n- question: a\n  answer: b", true}, // yaml doc marker
		{"{question: a}", true},                   // flow mapping
		{"", false},
	}
	for _, tc := range tests {
		if got := looksStructured([]byte(tc.in)); got != tc.want {
			t.Errorf("looksStructured(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseCardImport(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantCards   int
		wantSkipped int
		wantErr     bool
		// first[i] = expected {term, definition} of card i
		first map[int][2]string
	}{
		{
			name:    "csv is rejected (json/yaml only)",
			in:      "hund;dog\nkatze;cat\n",
			wantErr: true,
		},
		{
			name:      "json array",
			in:        `[{"question":"hund","answer":"dog"},{"question":"katze","answer":"cat"}]`,
			wantCards: 2,
			first:     map[int][2]string{1: {"katze", "cat"}},
		},
		{
			name:      "yaml list",
			in:        "- question: hund\n  answer: dog\n- question: katze\n  answer: cat\n",
			wantCards: 2,
			first:     map[int][2]string{0: {"hund", "dog"}},
		},
		{
			name:        "json skips missing fields",
			in:          `[{"question":"hund","answer":"dog"},{"question":"","answer":"x"},{"question":"y"}]`,
			wantCards:   1,
			wantSkipped: 2,
		},
		{
			name:    "malformed json/yaml errors",
			in:      "[ {question: ",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cards, skipped, err := parseCardImport([]byte(tc.in))
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(cards) != tc.wantCards || skipped != tc.wantSkipped {
				t.Fatalf("got %d cards / %d skipped, want %d / %d", len(cards), skipped, tc.wantCards, tc.wantSkipped)
			}
			for i, want := range tc.first {
				if cards[i].Term != want[0] || cards[i].Definition != want[1] {
					t.Errorf("card %d = {%q,%q}, want {%q,%q}", i, cards[i].Term, cards[i].Definition, want[0], want[1])
				}
			}
		})
	}
}

func TestParseTestImport(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantQs      int
		wantSkipped int
		wantErr     bool
	}{
		{
			name:    "csv is rejected (json/yaml only)",
			in:      "What is Go?;1;compiled;0;scripting\n",
			wantErr: true,
		},
		{
			name:   "json",
			in:     `[{"question":"Q?","options":[{"text":"a","correct":true},{"text":"b"}]}]`,
			wantQs: 1,
		},
		{
			name:   "yaml",
			in:     "- question: \"Q?\"\n  options:\n    - {text: a, correct: true}\n    - {text: b}\n",
			wantQs: 1,
		},
		{
			name:        "json skips <2 options and no correct",
			in:          `[{"question":"one","options":[{"text":"a","correct":true}]},{"question":"none","options":[{"text":"a"},{"text":"b"}]}]`,
			wantQs:      0,
			wantSkipped: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tqs, skipped, err := parseTestImport([]byte(tc.in))
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(tqs) != tc.wantQs || skipped != tc.wantSkipped {
				t.Fatalf("got %d questions / %d skipped, want %d / %d", len(tqs), skipped, tc.wantQs, tc.wantSkipped)
			}
		})
	}
}
