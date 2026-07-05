package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
	"gopkg.in/yaml.v3"
)

// blankMarker is the placeholder for a gap in an exercise sentence.
const blankMarker = "___"

// yamlExercise / yamlSentence mirror the agreed import contract. The file is a flat
// list of exercises (no envelope — the target collection is known from the URL).
type yamlExercise struct {
	Type        string         `yaml:"type"` // "bank" | "choice"
	Title       string         `yaml:"title"`
	Sentences   []yamlSentence `yaml:"sentences"`
	Distractors []string       `yaml:"distractors"` // bank only: extra words for the shared pool
}

type yamlSentence struct {
	Text        string     `yaml:"text"`
	Answer      []string   `yaml:"answer"`      // one word per blank, in order
	Distractors [][]string `yaml:"distractors"` // choice only: wrong options per blank
}

// RecordResults stores each answered sentence's words + correctness (no leveling).
func (h *CardsHandler) RecordResults(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Results []struct {
			SentenceID string   `json:"sentence_id"`
			Correct    bool     `json:"correct"`
			Submitted  []string `json:"submitted"`
		} `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	results := make([]service.SentenceResult, 0, len(body.Results))
	for _, x := range body.Results {
		if x.SentenceID == "" {
			continue
		}
		results = append(results, service.SentenceResult{SentenceID: x.SentenceID, Correct: x.Correct, Submitted: x.Submitted})
	}
	if err := h.svc.RecordSentenceResults(r.Context(), h.claims(r).UserID, results); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteExercise removes one exercise (and its sentences, via cascade) from a collection.
func (h *CardsHandler) DeleteExercise(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteExercise(r.Context(), chi.URLParam(r, "exID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetResults returns the user's saved answers for a collection's sentences.
func (h *CardsHandler) GetResults(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.GetExerciseResults(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ImportExercises godoc
// @Summary      Bulk import exercises from a YAML document
// @Tags         exercises
// @Accept       plain
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      201 {object} object{imported=int}
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/exercises/import [post]
func (h *CardsHandler) ImportExercises(w http.ResponseWriter, r *http.Request) {
	const maxBodySize = 4 << 20 // 4 MB
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		http.Error(w, "body too large", http.StatusBadRequest)
		return
	}

	var parsed []yamlExercise
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		http.Error(w, "invalid yaml", http.StatusBadRequest)
		return
	}

	// Build valid exercises, skipping malformed sentences/exercises (mirrors the CSV
	// import's row-skipping rather than failing the whole file).
	var exercises []model.Exercise
	sentenceCount := 0
	skipped := 0 // malformed sentences + dropped exercises, reported back to the user
	for _, ye := range parsed {
		kind := strings.TrimSpace(ye.Type)
		if kind != "bank" && kind != "choice" {
			skipped++
			continue
		}
		ex := model.Exercise{Kind: kind, Title: strings.TrimSpace(ye.Title)}
		if kind == "bank" {
			for _, d := range ye.Distractors {
				if d = strings.TrimSpace(d); d != "" {
					ex.Distractors = append(ex.Distractors, d)
				}
			}
		}
		for _, ys := range ye.Sentences {
			s, ok := buildSentence(kind, ys)
			if !ok {
				skipped++
				continue
			}
			ex.Sentences = append(ex.Sentences, s)
			sentenceCount++
		}
		if len(ex.Sentences) == 0 {
			skipped++
			continue
		}
		exercises = append(exercises, ex)
	}

	if len(exercises) == 0 {
		http.Error(w, "no valid exercises in file", http.StatusBadRequest)
		return
	}
	if sentenceCount > maxCSVRows {
		http.Error(w, "too many sentences", http.StatusBadRequest)
		return
	}

	cid, uid := chi.URLParam(r, "collectionID"), h.claims(r).UserID
	var impErr error
	if wantsDraft(r) {
		impErr = h.svc.StageImportExercises(r.Context(), cid, uid, exercises)
	} else {
		impErr = h.svc.ImportExercises(r.Context(), cid, uid, exercises)
	}
	if impErr != nil {
		handleErr(w, impErr)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(exercises), "skipped": skipped})
}

// buildSentence validates one sentence against its exercise kind and returns the model
// value. The number of answers must match the number of "___" blanks; for "choice" the
// distractors are wrong options per blank — one list per blank (so len == number of blanks).
func buildSentence(kind string, ys yamlSentence) (model.ExerciseSentence, bool) {
	text := strings.TrimSpace(ys.Text)
	nBlanks := strings.Count(text, blankMarker)
	if text == "" || nBlanks == 0 {
		return model.ExerciseSentence{}, false
	}

	answer := make([]string, 0, len(ys.Answer))
	for _, a := range ys.Answer {
		answer = append(answer, strings.TrimSpace(a))
	}
	if len(answer) != nBlanks {
		return model.ExerciseSentence{}, false
	}
	for _, a := range answer {
		if a == "" {
			return model.ExerciseSentence{}, false
		}
	}

	s := model.ExerciseSentence{Text: text, Answer: answer}

	if kind == "choice" && len(ys.Distractors) > 0 {
		// one list of wrong options per blank
		if len(ys.Distractors) != nBlanks {
			return model.ExerciseSentence{}, false
		}
		for _, gap := range ys.Distractors {
			words := make([]string, 0, len(gap))
			for _, word := range gap {
				if word = strings.TrimSpace(word); word != "" {
					words = append(words, word)
				}
			}
			s.Distractors = append(s.Distractors, words)
		}
	}
	return s, true
}
