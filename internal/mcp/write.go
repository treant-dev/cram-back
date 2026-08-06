package mcp

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/treant-dev/cram-go/internal/itemimport"
	"github.com/treant-dev/cram-go/internal/service"
)

// ---------- inputs ----------

// optionInput is one answer of a quiz.
type optionInput struct {
	Text string `json:"text"`
	// omitempty keeps `correct` out of the schema's required list: a model writing a wrong
	// option naturally omits it, and demanding "correct": false is a needless way to fail.
	Correct     bool   `json:"correct,omitempty" jsonschema:"True for the right answer. Omit it for wrong options."`
	Explanation string `json:"explanation,omitempty" jsonschema:"Optional note shown after answering."`
}

type sentenceInput struct {
	Text        string     `json:"text" jsonschema:"The sentence, with each gap written as three underscores (___)."`
	Answer      []string   `json:"answer" jsonschema:"One correct word per gap, in the order the gaps appear. Its length must equal the number of ___ in text."`
	Distractors [][]string `json:"distractors,omitempty" jsonschema:"For choice exercises only: one list of wrong options per gap, so its length must equal the number of gaps. Ignored when kind is bank."`
}

// itemInput mirrors the JSON import contract, so the same body works here and in the web UI's
// import panel.
type itemInput struct {
	Type string `json:"type" jsonschema:"card, quiz or exercise. A quiz is a multiple-choice question; an exercise is a set of fill-in-the-blank sentences."`

	Term       string `json:"term,omitempty" jsonschema:"card: the prompt side — the word or question being learned."`
	Definition string `json:"definition,omitempty" jsonschema:"card: the answer side — the translation, meaning or explanation."`

	Question string        `json:"question,omitempty" jsonschema:"quiz: the question text."`
	Options  []optionInput `json:"options,omitempty" jsonschema:"quiz: at least two options, at least one with correct=true."`

	Kind        string          `json:"kind,omitempty" jsonschema:"exercise: bank means all sentences share one shuffled word pool; choice means each gap offers its own options."`
	Title       string          `json:"title,omitempty" jsonschema:"exercise: optional heading."`
	Sentences   []sentenceInput `json:"sentences,omitempty" jsonschema:"exercise: the fill-in-the-blank sentences."`
	Distractors []string        `json:"distractors,omitempty" jsonschema:"exercise, kind=bank only: extra words mixed into the shared pool to make it harder."`
}

type createCollectionInput struct {
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	IsPublic    bool        `json:"is_public,omitempty"`
	Items       []itemInput `json:"items,omitempty" jsonschema:"Initial items, published immediately. A collection may mix cards, quizzes and exercises."`
}

type addItemsInput struct {
	CollectionID string      `json:"collection_id"`
	Items        []itemInput `json:"items" jsonschema:"Only the new items — existing ones are untouched, never resend them."`
}

// ---------- outputs ----------

type writeResult struct {
	CollectionID string `json:"collection_id"`
	Added        int    `json:"added"`
	Published    bool   `json:"published" jsonschema:"True when the items are live. False means they are staged and the user cannot see them yet."`
	Message      string `json:"message" jsonschema:"Plain-language summary to relay to the user, including what to do next when the items are not published."`
}

// ---------- registration ----------

func (s *session) addWriteTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "create_collection",
		Description: "Create a new collection, optionally filled in the same call. Items may mix cards " +
			"(term + definition), quizzes (multiple-choice questions) and exercises (fill-in-the-blank " +
			"sentences) — a collection is not limited to one kind. Content created this way is published " +
			"immediately; there is nothing to review yet. To add to an existing collection use add_items " +
			"or stage_items.",
	}, s.createCollection)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "add_items",
		Description: "Append items to an existing collection and publish them right away — the user sees " +
			"them immediately. Use this when the user asked for the content, not for a proposal. " +
			"If they want to look before it lands, use stage_items instead.",
	}, s.addItems)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "stage_items",
		Description: "Append items to a collection's draft instead of publishing them. Nothing is visible " +
			"to the user until publish_draft. Use this whenever the user wants to review first. The draft " +
			"is shared with the web UI's edit mode, so it may already hold their own unfinished changes — " +
			"the result says how many were there.",
	}, s.stageItems)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "publish_draft",
		Description: "Publish everything staged for a collection, making it the live version. " +
			"Call get_draft_changes first if the user has not seen what is pending.",
	}, s.publishDraft)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "discard_draft",
		Description: "Throw away everything staged for a collection, leaving the published version alone. " +
			"Destructive: it reports how many staged changes it dropped, including any the user made in " +
			"the web UI.",
	}, s.discardDraft)
}

// ---------- guards ----------

func (s *session) requireWrite() error {
	if !service.CanWrite(s.scope) {
		return errors.New("this token is read-only; create a token with read and write access in Cram settings to make changes")
	}
	return nil
}

// buildItems validates the whole batch before anything is written. Same validator the JSON import
// uses (internal/itemimport), so the rules cannot drift; the difference is policy — the import
// skips bad entries, here the model is told what to fix.
func buildItems(in []itemInput) ([]service.ImportItem, error) {
	if len(in) == 0 {
		return nil, errors.New("items is empty; nothing to add")
	}
	out := make([]service.ImportItem, 0, len(in))
	for i, it := range in {
		entry := itemimport.Entry{
			Type: it.Type, Term: it.Term, Definition: it.Definition,
			Question: it.Question, Kind: it.Kind, Title: it.Title, Distractors: it.Distractors,
		}
		for _, o := range it.Options {
			entry.Options = append(entry.Options, itemimport.Option{Text: o.Text, Correct: o.Correct, Explanation: o.Explanation})
		}
		for _, sn := range it.Sentences {
			entry.Sentences = append(entry.Sentences, itemimport.Sentence{Text: sn.Text, Answer: sn.Answer, Distractors: sn.Distractors})
		}
		built, err := itemimport.BuildItem(entry)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}
		out = append(out, built)
	}
	return out, nil
}

// stagedCount reports how many changes are already staged, so a result can warn about work that
// is not ours.
func (s *session) stagedCount(ctx context.Context, collectionID string) int {
	diff, err := s.deps.Collections.GetDraftDiff(ctx, collectionID, s.userID)
	if err != nil || diff == nil {
		return 0
	}
	return len(diff.Entries)
}

// ---------- handlers ----------

func (s *session) createCollection(ctx context.Context, _ *sdk.CallToolRequest, in createCollectionInput) (*sdk.CallToolResult, writeResult, error) {
	if err := s.requireWrite(); err != nil {
		return nil, writeResult{}, err
	}
	if in.Title == "" {
		return nil, writeResult{}, errors.New("title is required")
	}

	var items []service.ImportItem
	if len(in.Items) > 0 {
		built, err := buildItems(in.Items)
		if err != nil {
			// Validate before creating, so a bad batch does not leave an empty collection behind.
			return nil, writeResult{}, err
		}
		items = built
	}

	col, err := s.deps.Collections.CreateCollection(ctx, s.userID, in.Title, in.Description, "", in.IsPublic)
	if err != nil {
		return nil, writeResult{}, toolErr(err)
	}

	added := 0
	if len(items) > 0 {
		if added, err = s.deps.Collections.ImportItems(ctx, col.ID, s.userID, items, false); err != nil {
			return nil, writeResult{}, toolErr(err)
		}
	}
	return nil, writeResult{
		CollectionID: col.ID,
		Added:        added,
		Published:    true,
		Message:      fmt.Sprintf("Created %q with %d item(s), live now.", col.Title, added),
	}, nil
}

func (s *session) addItems(ctx context.Context, _ *sdk.CallToolRequest, in addItemsInput) (*sdk.CallToolResult, writeResult, error) {
	if err := s.requireWrite(); err != nil {
		return nil, writeResult{}, err
	}
	if in.CollectionID == "" {
		return nil, writeResult{}, errors.New("collection_id is required; call list_collections to find it")
	}
	items, err := buildItems(in.Items)
	if err != nil {
		return nil, writeResult{}, err
	}

	added, err := s.deps.Collections.ImportItems(ctx, in.CollectionID, s.userID, items, false)
	if err != nil {
		return nil, writeResult{}, toolErr(err)
	}

	res := writeResult{CollectionID: in.CollectionID, Added: added, Published: true}
	res.Message = fmt.Sprintf("Added %d item(s), live now.", added)
	// Publishing an unrelated pending draft is the user's likely next move; say it is there so
	// the model does not report a clean state that isn't one.
	if staged := s.stagedCount(ctx, in.CollectionID); staged > 0 {
		res.Message += fmt.Sprintf(" Note: this collection also has %d unpublished change(s) staged, untouched by this call.", staged)
	}
	return nil, res, nil
}

func (s *session) stageItems(ctx context.Context, _ *sdk.CallToolRequest, in addItemsInput) (*sdk.CallToolResult, writeResult, error) {
	if err := s.requireWrite(); err != nil {
		return nil, writeResult{}, err
	}
	if in.CollectionID == "" {
		return nil, writeResult{}, errors.New("collection_id is required; call list_collections to find it")
	}
	items, err := buildItems(in.Items)
	if err != nil {
		return nil, writeResult{}, err
	}

	before := s.stagedCount(ctx, in.CollectionID)
	added, err := s.deps.Collections.ImportItems(ctx, in.CollectionID, s.userID, items, true)
	if err != nil {
		return nil, writeResult{}, toolErr(err)
	}

	res := writeResult{CollectionID: in.CollectionID, Added: added, Published: false}
	res.Message = fmt.Sprintf(
		"Staged %d item(s). The user will NOT see them until the draft is published — call publish_draft, "+
			"or discard_draft to throw them away.", added)
	if before > 0 {
		res.Message += fmt.Sprintf(
			" This collection already had %d staged change(s) before this call, possibly the user's own "+
				"unfinished edits; publishing will release those too. get_draft_changes shows everything pending.", before)
	}
	return nil, res, nil
}

func (s *session) publishDraft(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, writeResult, error) {
	if err := s.requireWrite(); err != nil {
		return nil, writeResult{}, err
	}
	if in.CollectionID == "" {
		return nil, writeResult{}, errors.New("collection_id is required")
	}
	staged := s.stagedCount(ctx, in.CollectionID)
	if staged == 0 {
		return nil, writeResult{}, errors.New("nothing is staged for this collection")
	}
	if err := s.deps.Collections.PublishDraft(ctx, in.CollectionID, s.userID); err != nil {
		return nil, writeResult{}, toolErr(err)
	}
	return nil, writeResult{
		CollectionID: in.CollectionID,
		Added:        staged,
		Published:    true,
		Message:      fmt.Sprintf("Published %d staged change(s); they are live now.", staged),
	}, nil
}

func (s *session) discardDraft(ctx context.Context, _ *sdk.CallToolRequest, in collectionIDInput) (*sdk.CallToolResult, writeResult, error) {
	if err := s.requireWrite(); err != nil {
		return nil, writeResult{}, err
	}
	if in.CollectionID == "" {
		return nil, writeResult{}, errors.New("collection_id is required")
	}
	staged := s.stagedCount(ctx, in.CollectionID)
	if staged == 0 {
		return nil, writeResult{}, errors.New("nothing is staged for this collection")
	}
	if err := s.deps.Collections.DiscardDraft(ctx, in.CollectionID, s.userID); err != nil {
		return nil, writeResult{}, toolErr(err)
	}
	return nil, writeResult{
		CollectionID: in.CollectionID,
		Added:        0,
		Published:    true,
		Message: fmt.Sprintf(
			"Discarded %d staged change(s), including any the user had made in the web UI. The published version is unchanged.",
			staged),
	}, nil
}
