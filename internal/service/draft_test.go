package service

import (
	"context"
	"testing"

	"github.com/treant-dev/cram-go/internal/model"
)

func sp(s string) *string { return &s }

func TestSameItem(t *testing.T) {
	base := model.Item{Type: "card", Content: map[string]any{"term": "a", "definition": "b"}, Rank: "m"}
	tests := []struct {
		name string
		b    model.Item
		want bool
	}{
		{"identical", model.Item{Type: "card", Content: map[string]any{"term": "a", "definition": "b"}, Rank: "m"}, true},
		{"rank differs only", model.Item{Type: "card", Content: map[string]any{"term": "a", "definition": "b"}, Rank: "z"}, true},
		{"content differs", model.Item{Type: "card", Content: map[string]any{"term": "a", "definition": "X"}, Rank: "m"}, false},
		{"type differs", model.Item{Type: "exercise", Content: map[string]any{"term": "a", "definition": "b"}, Rank: "m"}, false},
		{"parent differs", model.Item{Type: "card", ParentID: sp("p"), Content: map[string]any{"term": "a", "definition": "b"}, Rank: "m"}, false},
	}
	for _, tc := range tests {
		if got := sameItem(base, tc.b); got != tc.want {
			t.Errorf("%s: sameItem = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// --- stubs (same package → can set unexported fields; embed interfaces so unused
// methods stay nil and simply must not be called on these paths) ---

type stubColl struct{ collectionRepo }

func (stubColl) ExistsForUser(context.Context, string, string) (bool, error) { return true, nil }

type listItems struct {
	itemRepo
	list []model.Item
}

func (l listItems) ListByCollection(context.Context, string) ([]model.Item, error) { return l.list, nil }

type listDrafts struct {
	itemDraftRepo
	list []model.ItemDraft
}

func (l listDrafts) ListByCollection(context.Context, string) ([]model.ItemDraft, error) {
	return l.list, nil
}

func TestGetDraftDiff(t *testing.T) {
	cid := "col"
	live := []model.Item{
		{ID: "keep", Type: "card", Content: map[string]any{"term": "k"}, Rank: "a"},
		{ID: "edit", Type: "card", Content: map[string]any{"term": "old"}, Rank: "b"},
		{ID: "del", Type: "card", Content: map[string]any{"term": "d"}, Rank: "c"},
		{ID: "same", Type: "card", Content: map[string]any{"term": "s"}, Rank: "d"},
	}
	drafts := []model.ItemDraft{
		{ItemID: "new", CollectionID: cid, Op: "upsert", Type: sp("card"), Content: map[string]any{"term": "n"}, Rank: sp("e")},
		{ItemID: "edit", CollectionID: cid, Op: "upsert", Type: sp("card"), Content: map[string]any{"term": "new"}, Rank: sp("b")},
		{ItemID: "del", CollectionID: cid, Op: "delete"},
		{ItemID: "same", CollectionID: cid, Op: "upsert", Type: sp("card"), Content: map[string]any{"term": "s"}, Rank: sp("d")},
	}
	svc := &CollectionService{collections: stubColl{}, items: listItems{list: live}, itemDrafts: listDrafts{list: drafts}}

	diff, err := svc.GetDraftDiff(context.Background(), cid, "user")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range diff.Entries {
		got[e.ItemID] = e.Status
	}
	want := map[string]string{"new": "added", "edit": "changed", "del": "deleted"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want exactly %v (unchanged must be omitted)", got, want)
	}
	for id, st := range want {
		if got[id] != st {
			t.Errorf("item %s: status %q, want %q", id, got[id], st)
		}
	}
	for _, e := range diff.Entries {
		switch e.ItemID {
		case "new":
			if e.Before != nil || e.After == nil {
				t.Errorf("added: want before=nil, after!=nil")
			}
		case "del":
			if e.Before == nil || e.After != nil {
				t.Errorf("deleted: want before!=nil, after=nil")
			}
		case "edit":
			if e.Before == nil || e.After == nil || str(e.Before.Content["term"]) != "old" || str(e.After.Content["term"]) != "new" {
				t.Errorf("changed: before/after wrong")
			}
		}
	}
}

type getItems struct {
	itemRepo
	live *model.Item
	last string
}

func (g getItems) Get(context.Context, string, string) (*model.Item, error) { return g.live, nil }
func (g getItems) LastRank(context.Context, string, *string) (string, error) { return g.last, nil }

type recDrafts struct {
	itemDraftRepo
	set      *model.ItemDraft
	existing *model.ItemDraft
	removed  bool
}

func (r *recDrafts) Set(_ context.Context, d model.ItemDraft) error { r.set = &d; return nil }
func (r *recDrafts) Get(context.Context, string) (*model.ItemDraft, error) { return r.existing, nil }
func (r *recDrafts) Remove(context.Context, string, string) error { r.removed = true; return nil }

func TestStageDraftDelete(t *testing.T) {
	// live item → delete marker, no removal
	rd := &recDrafts{}
	svc := &CollectionService{collections: stubColl{}, items: getItems{live: &model.Item{ID: "x", Type: "card"}}, itemDrafts: rd}
	if err := svc.StageDraftDelete(context.Background(), "col", "u", "x"); err != nil {
		t.Fatal(err)
	}
	if rd.set == nil || rd.set.Op != "delete" {
		t.Errorf("live item: want delete marker, got %+v", rd.set)
	}
	if rd.removed {
		t.Errorf("live item: should not remove the draft row")
	}

	// draft-only add (nothing live) → drop the draft row, no delete marker
	rd2 := &recDrafts{}
	svc2 := &CollectionService{collections: stubColl{}, items: getItems{live: nil}, itemDrafts: rd2}
	if err := svc2.StageDraftDelete(context.Background(), "col", "u", "y"); err != nil {
		t.Fatal(err)
	}
	if !rd2.removed {
		t.Errorf("draft-only add: want the draft row removed")
	}
	if rd2.set != nil {
		t.Errorf("draft-only add: should not stage a delete marker")
	}
}

func TestStageDraftItemRank(t *testing.T) {
	// editing an existing live item preserves its rank
	rd := &recDrafts{}
	svc := &CollectionService{collections: stubColl{}, items: getItems{live: &model.Item{ID: "x", Rank: "m"}, last: "z"}, itemDrafts: rd}
	it, err := svc.StageDraftItem(context.Background(), "col", "u", "x", DraftItemInput{Type: "card", Content: map[string]any{"term": "t"}})
	if err != nil {
		t.Fatal(err)
	}
	if it.Rank != "m" {
		t.Errorf("existing item: rank = %q, want m (preserved)", it.Rank)
	}

	// a new item gets an id and appends after the last rank
	rd2 := &recDrafts{}
	svc2 := &CollectionService{collections: stubColl{}, items: getItems{live: nil, last: "m"}, itemDrafts: rd2}
	it2, err := svc2.StageDraftItem(context.Background(), "col", "u", "", DraftItemInput{Type: "card", Content: map[string]any{"term": "t"}})
	if err != nil {
		t.Fatal(err)
	}
	if it2.ID == "" {
		t.Errorf("new item: expected a generated id")
	}
	if it2.Rank <= "m" {
		t.Errorf("new item: rank = %q, want > m (appended)", it2.Rank)
	}
	if rd2.set == nil || rd2.set.Op != "upsert" {
		t.Errorf("new item: expected an upsert draft row")
	}
}

func TestStageDraftItemRejectsBadType(t *testing.T) {
	svc := &CollectionService{collections: stubColl{}, items: getItems{}, itemDrafts: &recDrafts{}}
	_, err := svc.StageDraftItem(context.Background(), "col", "u", "", DraftItemInput{Type: "bogus", Content: map[string]any{}})
	if err != ErrInvalidType {
		t.Errorf("bad type: err = %v, want ErrInvalidType", err)
	}
}
