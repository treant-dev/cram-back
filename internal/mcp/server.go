// Package mcp exposes Cram collections to MCP-capable LLM clients over Streamable HTTP.
//
// Design notes worth knowing before changing anything here:
//
//   - The transport runs in stateless mode. Every request builds its own server bound to the
//     token that authenticated *that* request, so a session id can never carry one user's
//     identity into another user's call. Stateless also matches where the spec is heading
//     (SEP-2567). The cost is no server-initiated requests, which no tool here needs.
//   - Tools call internal/service directly, so ownership checks and error semantics match the
//     REST API exactly — it is the same code.
//   - Content is the unified item model: everything is an `item` with a Type and a JSONB body
//     (see internal/model/item.go). Tools speak that model rather than reconstructing the
//     pre-cutover card/test/exercise split, so what a model reads back matches what it wrote.
//   - Descriptions are part of the contract: a model has no other source for Cram's vocabulary,
//     so they spell it out and say when to call the tool, not just what it does.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
	"github.com/treant-dev/cram-go/internal/service"
)

const (
	serverName    = "cram"
	serverVersion = "0.1.0"

	// maxItems caps how much of a collection is returned, so one oversized collection cannot
	// swallow a client's context window. The result says how many items were withheld.
	maxItems = 200

	// toolTimeout bounds a single tool call. The transport clears the connection write deadline
	// (see withoutWriteDeadline), so without this a wedged call would hold a connection with no
	// ceiling at all.
	toolTimeout = 2 * time.Minute
)

// Deps are the services the tools need.
type Deps struct {
	Collections *service.CollectionService
}

// Handler builds the /mcp handler. Mount it behind middleware.RequireToken: this handler assumes
// claims are already in the request context and does no authentication of its own.
func Handler(deps Deps) http.Handler {
	h := sdk.NewStreamableHTTPHandler(
		func(r *http.Request) *sdk.Server { return newServer(deps, r) },
		&sdk.StreamableHTTPOptions{
			Stateless: true,
			// DNS-rebinding defence: a cross-site Origin is refused, while a request with no
			// Origin at all is allowed — non-browser MCP clients send none.
			CrossOriginProtection: &http.CrossOriginProtection{},
		},
	)
	return withDeadlines(h)
}

// withDeadlines swaps the API-wide response deadline for a request-scoped one.
//
// cmd/api sets http.Server.WriteTimeout as a blanket guard against slow clients, but that is a
// deadline on the whole response rather than on a single write: an SSE stream, or a tool call
// slower than the timeout, dies mid-response. Clearing it scopes the exception to /mcp; the
// context timeout then puts a ceiling back, so "no write deadline" does not mean "forever".
func withDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.SetWriteDeadline(time.Time{})
		}
		ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// session carries the identity behind one request.
type session struct {
	deps   Deps
	userID string
	scope  string
}

func newServer(deps Deps, r *http.Request) *sdk.Server {
	claims, _ := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	s := &session{deps: deps, scope: middleware.Scope(r)}
	if claims != nil {
		s.userID = claims.UserID
	}

	srv := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	s.addReadTools(srv)
	return srv
}

// toolErr converts a service error into something a model can act on, without leaking internals.
func toolErr(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return errors.New("no such collection, or it does not belong to you")
	case errors.Is(err, service.ErrForbidden):
		return errors.New("not allowed for this account")
	case errors.Is(err, service.ErrInvalidType):
		return errors.New("that item type is not accepted here")
	default:
		return fmt.Errorf("request failed: %w", err)
	}
}

// ---------- shared shapes ----------

type collectionIDInput struct {
	CollectionID string `json:"collection_id" jsonschema:"The id of the collection, as returned by list_collections."`
}

type collectionSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	HasDraft    bool   `json:"has_draft" jsonschema:"True when unpublished changes are staged for this collection."`
	UpdatedAt   string `json:"updated_at"`
}

// itemOut mirrors model.Item: a type plus a free-form body. Content is passed through rather than
// flattened into per-type fields, so the shape a model reads is the shape it writes back.
type itemOut struct {
	ID       string         `json:"id"`
	Type     string         `json:"type" jsonschema:"card, exercise or sentence. A sentence belongs to the exercise named by parent_id."`
	ParentID string         `json:"parent_id,omitempty"`
	Content  map[string]any `json:"content" jsonschema:"Type-specific body. card: term, definition. exercise: kind (bank|choice|quiz), title, and for quiz also question and options. sentence: text with ___ blanks, answer (one word per blank)."`
}

type listCollectionsOutput struct {
	Collections []collectionSummary `json:"collections"`
}

type getCollectionOutput struct {
	Collection     collectionSummary `json:"collection"`
	Items          []itemOut         `json:"items"`
	TotalItems     int               `json:"total_items"`
	TruncatedItems int               `json:"truncated_items" jsonschema:"Items withheld to keep the response small. Above zero means the collection is larger than what is shown."`
}

type getDraftOutput struct {
	CollectionID  string    `json:"collection_id"`
	HasDraft      bool      `json:"has_draft"`
	Items         []itemOut `json:"items" jsonschema:"The collection as it would look after publishing: live items with staged changes applied."`
	StagedChanges int       `json:"staged_changes"`
	Message       string    `json:"message"`
}

type draftChange struct {
	ItemID string   `json:"item_id"`
	Status string   `json:"status" jsonschema:"added, changed or deleted."`
	Before *itemOut `json:"before,omitempty" jsonschema:"Published state; absent when the item is newly added."`
	After  *itemOut `json:"after,omitempty" jsonschema:"Staged state; absent when the item is being deleted."`
}

type getDraftChangesOutput struct {
	CollectionID string        `json:"collection_id"`
	Changes      []draftChange `json:"changes"`
	Message      string        `json:"message"`
}

type progressEntryOut struct {
	ItemID       string `json:"item_id"`
	Level        int    `json:"level" jsonschema:"Higher means better recalled; 0 means still being learned."`
	NextReviewAt string `json:"next_review_at"`
	LastReviewAt string `json:"last_review_at,omitempty"`
}

type getProgressOutput struct {
	Cards   []progressEntryOut `json:"cards"`
	Weakest []string           `json:"weakest" jsonschema:"Ids of the lowest-level cards, weakest first — the ones worth practising."`
	Message string             `json:"message"`
}

// ---------- registration ----------

func (s *session) addReadTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "list_collections",
		Description: "List the study collections belonging to the current Cram user. " +
			"A collection holds items: cards (term + definition), exercises (fill-in-the-blank " +
			"sentences, or a quiz — a multiple-choice question), and the sentences belonging to an " +
			"exercise. One collection can hold a mix. Call this first to find a collection's id.",
	}, s.listCollections)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_collection",
		Description: "Read a collection's published items. Returns each item's type and body as stored. " +
			"Unpublished changes are NOT included — use get_draft for those. Large collections are " +
			"truncated, so check truncated_items before concluding you have seen everything.",
	}, s.getCollection)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_draft",
		Description: "Read a collection as it would look after publishing: live items with any staged " +
			"changes applied. Use it to check what is pending before publishing, especially when the " +
			"user may have unfinished edits of their own in the web UI. Reading never changes anything.",
	}, s.getDraft)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_draft_changes",
		Description: "List only what differs between the staged draft and the published collection — " +
			"each item marked added, changed or deleted, with its before and after. This is what to read " +
			"back to the user before publishing; it is far shorter than the whole collection.",
	}, s.getDraftChanges)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_progress",
		Description: "Read the user's spaced-repetition progress for a collection: per-card level and " +
			"when it is next due. Cards only — exercises and quizzes are not levelled, so a collection " +
			"without cards returns nothing. Items the user has never studied are absent.",
	}, s.getProgress)
}

// ---------- handlers ----------

func (s *session) listCollections(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, listCollectionsOutput, error) {
	cols, err := s.deps.Collections.ListCollections(ctx, s.userID)
	if err != nil {
		return nil, listCollectionsOutput{}, toolErr(err)
	}
	out := listCollectionsOutput{Collections: make([]collectionSummary, 0, len(cols))}
	for _, c := range cols {
		out.Collections = append(out.Collections, toSummary(c))
	}
	return nil, out, nil
}

func (s *session) getCollection(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, getCollectionOutput, error) {
	if in.CollectionID == "" {
		return nil, getCollectionOutput{}, errors.New("collection_id is required; call list_collections to find it")
	}
	// isAdmin is deliberately false: an admin's token grants no cross-user reach here.
	col, err := s.deps.Collections.GetCollection(ctx, in.CollectionID, s.userID, false)
	if err != nil {
		return nil, getCollectionOutput{}, toolErr(err)
	}

	items, truncated := toItems(col.Items)
	return nil, getCollectionOutput{
		Collection:     toSummary(*col),
		Items:          items,
		TotalItems:     len(col.Items),
		TruncatedItems: truncated,
	}, nil
}

func (s *session) getDraft(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, getDraftOutput, error) {
	if in.CollectionID == "" {
		return nil, getDraftOutput{}, errors.New("collection_id is required")
	}
	// Despite the name this only reads: a draft is an overlay of item_draft rows over live
	// items, so there is no row to create just by looking.
	col, err := s.deps.Collections.GetOrCreateDraft(ctx, in.CollectionID, s.userID)
	if err != nil {
		return nil, getDraftOutput{}, toolErr(err)
	}
	diff, err := s.deps.Collections.GetDraftDiff(ctx, in.CollectionID, s.userID)
	if err != nil {
		return nil, getDraftOutput{}, toolErr(err)
	}

	items, _ := toItems(col.Items)
	out := getDraftOutput{
		CollectionID:  in.CollectionID,
		HasDraft:      len(diff.Entries) > 0,
		Items:         items,
		StagedChanges: len(diff.Entries),
	}
	if out.HasDraft {
		out.Message = fmt.Sprintf(
			"%q has %d staged change(s) not yet visible to the user. Use get_draft_changes to see just the delta.",
			col.Title, len(diff.Entries))
	} else {
		out.Message = fmt.Sprintf("%q has nothing staged; what you see is published.", col.Title)
	}
	return nil, out, nil
}

func (s *session) getDraftChanges(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, getDraftChangesOutput, error) {
	if in.CollectionID == "" {
		return nil, getDraftChangesOutput{}, errors.New("collection_id is required")
	}
	diff, err := s.deps.Collections.GetDraftDiff(ctx, in.CollectionID, s.userID)
	if err != nil {
		return nil, getDraftChangesOutput{}, toolErr(err)
	}

	out := getDraftChangesOutput{CollectionID: in.CollectionID, Changes: make([]draftChange, 0, len(diff.Entries))}
	for _, e := range diff.Entries {
		out.Changes = append(out.Changes, draftChange{
			ItemID: e.ItemID,
			Status: e.Status,
			Before: toItemPtr(e.Before),
			After:  toItemPtr(e.After),
		})
	}
	if len(out.Changes) == 0 {
		out.Message = "Nothing is staged for this collection."
	} else {
		out.Message = fmt.Sprintf("%d staged change(s), not visible to the user until published.", len(out.Changes))
	}
	return nil, out, nil
}

func (s *session) getProgress(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, getProgressOutput, error) {
	if in.CollectionID == "" {
		return nil, getProgressOutput{}, errors.New("collection_id is required")
	}
	data, err := s.deps.Collections.GetProgress(ctx, in.CollectionID, s.userID)
	if err != nil {
		return nil, getProgressOutput{}, toolErr(err)
	}

	out := getProgressOutput{
		Cards:   make([]progressEntryOut, 0, len(data.Cards)),
		Weakest: []string{},
	}
	for id, e := range data.Cards {
		out.Cards = append(out.Cards, toProgressEntry(id, e))
	}
	// Lowest level first, ties broken by id so repeated calls agree.
	sort.Slice(out.Cards, func(i, j int) bool {
		if out.Cards[i].Level != out.Cards[j].Level {
			return out.Cards[i].Level < out.Cards[j].Level
		}
		return out.Cards[i].ItemID < out.Cards[j].ItemID
	})
	for i, e := range out.Cards {
		if i == 10 {
			break
		}
		out.Weakest = append(out.Weakest, e.ItemID)
	}
	if len(out.Cards) == 0 {
		out.Message = "No progress recorded yet. Only cards are levelled — exercises and quizzes never appear here."
	} else {
		out.Message = fmt.Sprintf("Progress for %d card(s).", len(out.Cards))
	}
	return nil, out, nil
}

// ---------- mapping ----------

func toSummary(c model.Collection) collectionSummary {
	return collectionSummary{
		ID:          c.ID,
		Title:       c.Title,
		Description: c.Description,
		IsPublic:    c.IsPublic,
		HasDraft:    c.DraftID != nil,
		UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
	}
}

func toItem(it model.Item) itemOut {
	out := itemOut{ID: it.ID, Type: it.Type, Content: it.Content}
	if it.ParentID != nil {
		out.ParentID = *it.ParentID
	}
	return out
}

func toItemPtr(it *model.Item) *itemOut {
	if it == nil {
		return nil
	}
	v := toItem(*it)
	return &v
}

// toItems maps up to maxItems and reports how many were withheld.
func toItems(items []model.Item) ([]itemOut, int) {
	n := len(items)
	if n > maxItems {
		n = maxItems
	}
	out := make([]itemOut, 0, n)
	for _, it := range items[:n] {
		out = append(out, toItem(it))
	}
	return out, len(items) - n
}

func toProgressEntry(id string, e repository.ProgressEntry) progressEntryOut {
	out := progressEntryOut{ItemID: id, Level: e.Level, NextReviewAt: e.NextReviewAt.Format(time.RFC3339)}
	if e.LastReviewAt != nil {
		out.LastReviewAt = e.LastReviewAt.Format(time.RFC3339)
	}
	return out
}
