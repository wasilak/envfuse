package state

import "sync"

type Store struct {
	mu             sync.RWMutex
	lastKnownGood  map[string]map[string]any
	lastAppliedEnv map[string]string
	lastRenderedFiles map[string][]byte
	candidate      map[string]map[string]any
	envCandidate   map[string]string
	renderedCandidate map[string][]byte
}

func NewStore() *Store {
	return &Store{
		lastKnownGood: map[string]map[string]any{},
		lastAppliedEnv: map[string]string{},
		lastRenderedFiles: map[string][]byte{},
	}
}

// StageCandidate stages cycle output without mutating applied state, per SYNC-04.
func (s *Store) StageCandidate(candidate map[string]map[string]any, envCandidate map[string]string, renderedCandidate map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidate = cloneSnapshot(candidate)
	s.envCandidate = cloneEnv(envCandidate)
	s.renderedCandidate = cloneFiles(renderedCandidate)
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
	s.lastRenderedFiles = cloneFiles(s.renderedCandidate)
	s.candidate = nil
	s.envCandidate = nil
	s.renderedCandidate = nil
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

func (s *Store) LastRenderedFiles() map[string][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFiles(s.lastRenderedFiles)
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

func cloneFiles(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		buf := make([]byte, len(v))
		copy(buf, v)
		out[k] = buf
	}
	return out
}
