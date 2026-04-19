package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// stateStore is an in-memory CSRF state store.
// Replace with Redis for multi-instance deployments.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

var store = &stateStore{entries: make(map[string]time.Time)}

const stateTTL = 10 * time.Minute

func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := base64.URLEncoding.EncodeToString(b)

	store.mu.Lock()
	store.entries[state] = time.Now().Add(stateTTL)
	store.mu.Unlock()

	return state, nil
}

func ValidateState(state string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()

	exp, ok := store.entries[state]
	if !ok {
		return false
	}
	delete(store.entries, state)
	return time.Now().Before(exp)
}
