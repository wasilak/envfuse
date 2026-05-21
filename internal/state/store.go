package state

import "sync"

type Store struct {
	mu             sync.RWMutex
	lastKnownGood  map[string]map[string]any
	lastAppliedEnv map[string]string
	candidate      map[string]map[string]any
	envCandidate   map[string]string
}

func NewStore() *Store {
	return &Store{
		lastKnownGood: map[string]map[string]any{},
		lastAppliedEnv: map[string]string{},
	}
}

// StageCandidate stages cycle output without mutating applied state, per SYNC-04.
func (s *Store) StageCandidate(candidate map[string]map[string]any, envCandidate map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidate = cloneSnapshot(candidate)
	s.envCandidate = cloneEnv(envCandidate)
}

// CommitCandidate promotes staged output only after full-batch success, per SYNC-03.
func (s *Store) CommitCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.candidate == nil {
		return
	}

	s.lastKnownGood = cloneSnapshot(s.candidate)
	s.lastAppliedEnv = cloneEnv(s.envCandidate)
	s.candidate = nil
	s.envCandidate = nil
}

func (s *Store) LastKnownGood() map[string]map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.lastKnownGood)
}

func (s *Store) LastAppliedEnv() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEnv(s.lastAppliedEnv)
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

func cloneEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
