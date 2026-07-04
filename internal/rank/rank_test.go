package rank

import "testing"

func TestBetweenOrdering(t *testing.T) {
	cases := []struct{ a, b string }{
		{"", ""},
		{"i", ""},   // append after
		{"", "i"},   // prepend before
		{"a", "b"},  // adjacent chars → descends
		{"az", "b"},
		{"i", "j"},
	}
	for _, c := range cases {
		got := Between(c.a, c.b)
		if c.a != "" && !(c.a < got) {
			t.Errorf("Between(%q,%q)=%q: not > a", c.a, c.b, got)
		}
		if c.b != "" && !(got < c.b) {
			t.Errorf("Between(%q,%q)=%q: not < b", c.a, c.b, got)
		}
	}
}

func TestRepeatedInsertBetweenStaysOrdered(t *testing.T) {
	lo, hi := First(), After(First())
	if !(lo < hi) {
		t.Fatalf("First()=%q not < After(First())=%q", lo, hi)
	}
	// Insert 100 times between lo and hi; each must stay strictly between.
	for i := 0; i < 100; i++ {
		mid := Between(lo, hi)
		if !(lo < mid && mid < hi) {
			t.Fatalf("iter %d: %q < %q < %q violated", i, lo, mid, hi)
		}
		hi = mid // keep squeezing toward lo
	}
}

func TestSequenceAscending(t *testing.T) {
	keys := Sequence(50)
	for i := 1; i < len(keys); i++ {
		if !(keys[i-1] < keys[i]) {
			t.Fatalf("Sequence not ascending at %d: %q !< %q", i, keys[i-1], keys[i])
		}
	}
}
