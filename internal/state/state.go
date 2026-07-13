// Package state persists engine memory (last metric values, rule state,
// dedup timestamps) so a restart neither re-alerts nor loses `for` timers.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/madnh/emday/internal/model"
)

const schemaVersion = 1

type MetricState struct {
	Value   model.Value `json:"value"`
	Updated time.Time   `json:"updated"`
}

type RuleState struct {
	Firing         bool       `json:"firing"`
	PendingSince   *time.Time `json:"pending_since,omitempty"`
	ResolveSince   *time.Time `json:"resolve_since,omitempty"`
	LastFire       *time.Time `json:"last_fire,omitempty"`
	NotifiedFiring bool       `json:"notified_firing,omitempty"` // whether the fire notification was actually sent (vs suppressed by cooldown)
}

type State struct {
	Version int                     `json:"version"`
	Metrics map[string]*MetricState `json:"metrics"`
	Rules   map[string]*RuleState   `json:"rules"`
	Dedup   map[string]time.Time    `json:"dedup"` // event hash -> last sent

	path string
	mu   sync.Mutex
}

func empty(path string) *State {
	return &State{
		Version: schemaVersion,
		Metrics: map[string]*MetricState{},
		Rules:   map[string]*RuleState{},
		Dedup:   map[string]time.Time{},
		path:    path,
	}
}

// Load reads state from path; a missing file yields a fresh empty state.
func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty(path), nil
	}
	if err != nil {
		return nil, err
	}
	st := empty(path)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, st); err != nil {
			return nil, fmt.Errorf("%s is corrupt: %w (move it away to start fresh)", path, err)
		}
	}
	if st.Version > schemaVersion {
		return nil, fmt.Errorf("%s has state version %d, newer than this binary understands (%d) — upgrade emday", path, st.Version, schemaVersion)
	}
	if st.Metrics == nil {
		st.Metrics = map[string]*MetricState{}
	}
	if st.Rules == nil {
		st.Rules = map[string]*RuleState{}
	}
	if st.Dedup == nil {
		st.Dedup = map[string]time.Time{}
	}
	st.path = path
	return st, nil
}

// Save writes atomically (temp file + rename).
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneDedupLocked(48 * time.Hour)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *State) pruneDedupLocked(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	for k, t := range s.Dedup {
		if t.Before(cutoff) {
			delete(s.Dedup, k)
		}
	}
}

func (s *State) Metric(name string) (*MetricState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.Metrics[name]
	return m, ok
}

func (s *State) SetMetric(name string, v model.Value, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Metrics[name] = &MetricState{Value: v, Updated: at}
}

func (s *State) DeleteMetric(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Metrics, name)
}

// MetricsWithPrefix lists stored metric names under a source prefix,
// used to detect keys that disappeared between runs.
func (s *State) MetricsWithPrefix(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for name := range s.Metrics {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			out = append(out, name)
		}
	}
	return out
}

func (s *State) Rule(id string) *RuleState {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, ok := s.Rules[id]
	if !ok {
		rs = &RuleState{}
		s.Rules[id] = rs
	}
	return rs
}

// DedupAllows reports whether an event with this key may be sent now, and
// records the send time when allowed.
func (s *State) DedupAllows(key string, cooldown time.Duration, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.Dedup[key]; ok && now.Sub(last) < cooldown {
		return false
	}
	s.Dedup[key] = now
	return true
}

// EnsureDir makes sure the parent dir exists (used by init only).
func EnsureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
