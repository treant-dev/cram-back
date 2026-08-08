package service

import "testing"

// cardContent is the single choke point every card write passes through, so the hint's
// trimming and its absence when empty are settled here rather than at each caller.
func TestCardContentHint(t *testing.T) {
	tests := []struct {
		name    string
		hint    string
		want    string
		present bool
	}{
		{"a hint is carried", "masculine", "masculine", true},
		{"surrounding space is dropped", "  masculine\n", "masculine", true},
		{"no hint means no key", "", "", false},
		{"whitespace is no hint", "   ", "", false},
	}
	for _, tc := range tests {
		c := cardContent("der Löffel", "spoon", "", tc.hint)
		got, present := c["hint"]
		if present != tc.present {
			t.Errorf("%s: hint present = %v, want %v", tc.name, present, tc.present)
			continue
		}
		if present && got != tc.want {
			t.Errorf("%s: hint = %v, want %q", tc.name, got, tc.want)
		}
	}
}
