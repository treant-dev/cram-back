package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

// The editing tools are mostly guards: they exist to stop a model from doing damage that the
// service layer would happily accept. That makes them worth testing directly rather than through
// a running server, which is how these were checked before and why a lapsed cookie could look
// like a broken tool.

func TestDraftContent(t *testing.T) {
	t.Run("card", func(t *testing.T) {
		content, typ, err := draftContent(service.ImportItem{
			Type: "card", Card: &model.Card{Term: "hola", Definition: "hello"},
		})
		if err != nil {
			t.Fatalf("card: %v", err)
		}
		if typ != "card" || content["term"] != "hola" || content["definition"] != "hello" {
			t.Errorf("unexpected content: %v (%s)", content, typ)
		}
	})

	t.Run("quiz is stored as an exercise", func(t *testing.T) {
		content, typ, err := draftContent(service.ImportItem{
			Type: "quiz",
			Quiz: &model.TestQuestion{Question: "2+2?", Options: []model.TestAnswer{
				{Text: "4", IsCorrect: true}, {Text: "5"},
			}},
		})
		if err != nil {
			t.Fatalf("quiz: %v", err)
		}
		if typ != "exercise" || content["kind"] != "quiz" {
			t.Errorf("quiz should be an exercise of kind quiz, got %s / %v", typ, content["kind"])
		}
		opts, ok := content["options"].([]any)
		if !ok || len(opts) != 2 {
			t.Fatalf("options not carried: %v", content["options"])
		}
		if first := opts[0].(map[string]any); first["is_correct"] != true {
			t.Errorf("correct option lost: %v", first)
		}
	})

	// A bank or choice exercise owns child sentence items, which a single upsert cannot rewrite.
	// Refusing is the honest answer; silently updating the heading alone would look like success.
	t.Run("fill-in-the-blank exercise is refused with a reason", func(t *testing.T) {
		_, _, err := draftContent(service.ImportItem{
			Type: "exercise", Exercise: &model.Exercise{Kind: "bank"},
		})
		if err == nil || !strings.Contains(err.Error(), "separate items") {
			t.Errorf("got %v, want an explanation about sentences", err)
		}
	})
}

// The mismatch message must speak the caller's vocabulary: they wrote type "quiz", so being told
// their item "is a exercise" reads as a contradiction of what they just sent.
func TestDescribeItem(t *testing.T) {
	cases := []struct{ itemType, kind, want string }{
		{"card", "", "card"},
		{"exercise", "quiz", "quiz"},
		{"exercise", "bank", "bank exercise"},
		{"exercise", "choice", "choice exercise"},
		{"exercise", "", "exercise"},
		{"sentence", "", "sentence"},
	}
	for _, tc := range cases {
		if got := describeItem(tc.itemType, tc.kind); got != tc.want {
			t.Errorf("describeItem(%q, %q) = %q, want %q", tc.itemType, tc.kind, got, tc.want)
		}
	}
}

func TestUpdateCollectionRequiresSomethingToChange(t *testing.T) {
	s := &session{scope: model.ScopeReadWrite}
	_, _, err := s.updateCollection(context.Background(), nil, updateCollectionInput{CollectionID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("got %v, want a complaint that no field was given", err)
	}
}

func TestEditingToolsRefuseAReadOnlyToken(t *testing.T) {
	s := &session{scope: model.ScopeRead}
	ctx := context.Background()
	title := "x"

	calls := map[string]error{}
	_, _, calls["update_collection"] = s.updateCollection(ctx, nil, updateCollectionInput{CollectionID: "c", Title: &title})
	_, _, calls["update_item"] = s.updateItem(ctx, nil, updateItemInput{CollectionID: "c", ItemID: "i"})
	_, _, calls["delete_item"] = s.deleteItem(ctx, nil, itemRefInput{CollectionID: "c", ItemID: "i"})
	_, _, calls["delete_collection"] = s.deleteCollection(ctx, nil, collectionIDInput{CollectionID: "c"})

	for name, err := range calls {
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s with a read scope: got %v, want a read-only refusal", name, err)
		}
	}
}

func TestEditingToolsRequireIDs(t *testing.T) {
	s := &session{scope: model.ScopeReadWrite}
	ctx := context.Background()
	title := "x"

	if _, _, err := s.updateCollection(ctx, nil, updateCollectionInput{Title: &title}); err == nil {
		t.Error("update_collection accepted an empty collection_id")
	}
	// An item is addressed by id read back from the collection — never matched by its text, or a
	// near miss becomes an edit to the wrong card.
	if _, _, err := s.updateItem(ctx, nil, updateItemInput{CollectionID: "c"}); err == nil {
		t.Error("update_item accepted an empty item_id")
	}
	if _, _, err := s.deleteItem(ctx, nil, itemRefInput{CollectionID: "c"}); err == nil {
		t.Error("delete_item accepted an empty item_id")
	}
	if _, _, err := s.deleteCollection(ctx, nil, collectionIDInput{}); err == nil {
		t.Error("delete_collection accepted an empty collection_id")
	}
}
