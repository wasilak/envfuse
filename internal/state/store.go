package state

import "sync"

type Store struct {
	mu            sync.RWMutex
	lastKnownGood map[string]map[string]any
	candidate     map[string]map[string]any
}

func NewStore() *Store {
	return &Store{
		lastKnownGood: map[string]map[string]any{},
	}
}

// StageCandidate stages cycle output without mutating applied state, per SYNC-04.
func (s *Store) StageCandidate(candidate map[string]map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidate = cloneSnapshot(candidate)
}

// CommitCandidate promotes staged output only after full-batch success, per SYNC-03.
func (s *Store) CommitCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.candidate == nil {
		return
	}

	s.lastKnownGood = cloneSnapshot(s.candidate)
	s.candidate = nil
}

func (s *Store) LastKnownGood() map[string]map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.lastKnownGood)
}

func cloneSnapshot(in map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(in))
	for path, kv := range in {
		copyKV := make(map[string]any, len(kv))
		for k, v := range kv {
			copyKV[k] = v
		}
		out[path] = copyKV
	}
	return out
}
