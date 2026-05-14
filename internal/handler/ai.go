package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type AIHandler struct{}

func NewAIHandler() *AIHandler { return &AIHandler{} }

func (h *AIHandler) SuggestDefinition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Term string `json:"term"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Term == "" {
		http.Error(w, "term is required", http.StatusBadRequest)
		return
	}
	if len(body.Term) > maxFieldLen {
		http.Error(w, "term too long", http.StatusBadRequest)
		return
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		http.Error(w, "AI service not configured", http.StatusServiceUnavailable)
		return
	}

	payload := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 256,
		"messages": []map[string]string{{
			"role":    "user",
			"content": fmt.Sprintf("The user provided this term: \"%s\". If it contains a typo or misspelling, identify the correct spelling. Then write a definition in the style of the Cambridge Dictionary: formal, concise, one sentence. Rules: do not use the term or any of its word forms inside the definition, no trailing period, no quotes, no preamble. If the term was misspelled, prepend the corrected spelling in square brackets followed by a space, e.g. \"[bladder] a saclike organ...\". If the spelling was correct, return only the definition text.", body.Term),
		}},
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(payloadBytes))
	if err != nil {
		http.Error(w, "AI service error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "AI service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Content) == 0 {
		http.Error(w, "AI service error", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"definition": result.Content[0].Text})
}
