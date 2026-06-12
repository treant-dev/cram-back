package service

import (
	"testing"
	"time"
)

func TestProgressApplyAnswer(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	tests := []struct {
		name         string
		level        int
		correct      bool
		nextReviewAt time.Time
		want         int
	}{
		{"mastered stays mastered", 7, true, past, 7},
		{"mastered stays on wrong", 7, false, past, 7},
		{"level 1 always advances", 1, true, future, 2},
		{"correct but not due yet holds", 3, true, future, 3},
		{"correct and due advances", 3, true, past, 4},
		{"correct caps at 6", 6, true, past, 6},
		{"wrong halves", 6, false, past, 3},
		{"wrong never below 1", 1, false, past, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressApplyAnswer(tc.level, tc.correct, tc.nextReviewAt); got != tc.want {
				t.Errorf("progressApplyAnswer(%d, %v) = %d, want %d", tc.level, tc.correct, got, tc.want)
			}
		})
	}
}

func TestProgressApplyConfidence(t *testing.T) {
	tests := []struct {
		name  string
		level int
		delta int
		want  int
	}{
		{"raise from high masters", 6, 1, 7},
		{"raise increments", 3, 1, 4},
		{"lower halves", 4, -1, 2},
		{"lower never below 1", 1, -1, 1},
		{"no delta unchanged", 5, 0, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := progressApplyConfidence(tc.level, tc.delta); got != tc.want {
				t.Errorf("progressApplyConfidence(%d, %d) = %d, want %d", tc.level, tc.delta, got, tc.want)
			}
		})
	}
}

// progressRetryBump is the blitz "repeat the mistake" redemption: a correct retry
// adds +1 bypassing the due-date gate, capped at 6, and mastery stays mastered.
func TestProgressRetryBump(t *testing.T) {
	tests := []struct {
		level int
		want  int
	}{
		{1, 2},
		{3, 4},
		{5, 6},
		{6, 6}, // capped — a retry alone cannot master
		{7, 7}, // mastered stays
	}
	for _, tc := range tests {
		if got := progressRetryBump(tc.level); got != tc.want {
			t.Errorf("progressRetryBump(%d) = %d, want %d", tc.level, got, tc.want)
		}
	}
}

func TestProgressNextReview(t *testing.T) {
	now := time.Now()
	// Lower levels are reviewed sooner than higher levels.
	if !progressNextReview(1).Before(progressNextReview(3)) {
		t.Error("level 1 should be due before level 3")
	}
	if !progressNextReview(3).Before(progressNextReview(6)) {
		t.Error("level 3 should be due before level 6")
	}
	// Mastered (7) is effectively never due.
	if progressNextReview(7).Year() < 2099 {
		t.Errorf("level 7 should be far in the future, got %v", progressNextReview(7))
	}
	// All non-mastered reviews are in the future.
	for lvl := 1; lvl <= 6; lvl++ {
		if !progressNextReview(lvl).After(now) {
			t.Errorf("level %d next review should be after now", lvl)
		}
	}
}
