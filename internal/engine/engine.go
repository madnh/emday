// Package engine wires sources → rules → notifier queue and runs the
// scheduler. All alerting decisions (change detection, thresholds, `for`
// timers, resolved events, dedup) live here.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/expr-lang/expr/vm"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
	"github.com/madnh/emday/internal/notify"
	"github.com/madnh/emday/internal/source"
	"github.com/madnh/emday/internal/state"
)

type compiledRule struct {
	cfg  *config.Rule
	id   string
	prog *vm.Program // nil for on_change rules
}

type Engine struct {
	cfg     *config.Config
	st      *state.State
	queue   *notify.Queue
	sources map[string]source.Source
	rules   []*compiledRule // all rules, matched by metric name

	mu sync.Mutex // serializes Process across source goroutines
}

// New builds the engine. Config must already be validated; source types
// unsupported on this platform are skipped with a warning.
func New(cfg *config.Config, st *state.State, queue *notify.Queue) (*Engine, error) {
	e := &Engine{cfg: cfg, st: st, queue: queue, sources: map[string]source.Source{}}
	for name, sc := range cfg.Sources {
		src, err := source.New(name, sc, cfg.TmpDir())
		if err != nil {
			log.Printf("source %s disabled: %v", name, err)
			continue
		}
		e.sources[name] = src
	}
	for _, rc := range cfg.Rules {
		cr := &compiledRule{cfg: rc, id: rc.ID()}
		if rc.Condition != "" {
			prog, err := config.CompileCondition(rc.Condition)
			if err != nil {
				return nil, fmt.Errorf("rule %s: %w", rc.Metric, err)
			}
			cr.prog = prog
		}
		e.rules = append(e.rules, cr)
	}
	return e, nil
}

// Run starts one collect loop per source plus the queue dispatcher, and
// blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.queue.Run(ctx)
	}()

	for name, src := range e.sources {
		interval := e.cfg.Sources[name].Interval.Duration
		wg.Add(1)
		go func(name string, src source.Source, interval time.Duration) {
			defer wg.Done()
			e.collectLoop(ctx, name, src, interval)
		}(name, src, interval)
	}

	// housekeeping: stale exec tmp files, periodic state save
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				source.CleanupTmp(e.cfg.TmpDir(), 2*time.Hour)
			}
		}
	}()

	wg.Wait()
	if err := e.st.Save(); err != nil {
		log.Printf("saving state on shutdown: %v", err)
	}
}

func (e *Engine) collectLoop(ctx context.Context, name string, src source.Source, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	running := false
	var runMu sync.Mutex

	collect := func() {
		runMu.Lock()
		if running {
			runMu.Unlock()
			log.Printf("source %s: previous collect still running, skipping this tick", name)
			return
		}
		running = true
		runMu.Unlock()
		defer func() {
			runMu.Lock()
			running = false
			runMu.Unlock()
		}()

		samples, events, err := src.Collect(ctx)
		if err != nil {
			log.Printf("source %s: %v", name, err)
		}
		e.Process(name, samples, events)
	}

	collect()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

// Process runs samples through rules, forwards direct events, updates state.
// Exported for tests.
func (e *Engine) Process(sourceName string, samples []model.Sample, events []model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	// Direct NOTIFY_* events: dedup, then route to the source's targets.
	targets := e.cfg.Sources[sourceName].Notify
	for _, ev := range events {
		if len(targets) == 0 {
			log.Printf("source %s emitted a notification but has no `notify` targets: %s", sourceName, ev.Title)
			continue
		}
		key := dedupKey(ev)
		if !e.st.DedupAllows(key, e.cooldownFor(nil), now) {
			continue
		}
		e.dispatch(ev, targets)
	}

	seen := map[string]bool{}
	for _, s := range samples {
		seen[s.Metric] = true
		e.applyRules(s, now)
		e.st.SetMetric(s.Metric, s.Value, s.Time)
	}

	// Keys that disappeared since the last run are a change too.
	if len(samples) > 0 {
		for _, name := range e.st.MetricsWithPrefix(sourceName + ".") {
			if seen[name] {
				continue
			}
			prev, _ := e.st.Metric(name)
			e.fireOnChangeForAbsent(name, prev, now)
			e.st.DeleteMetric(name)
		}
	}

	if err := e.st.Save(); err != nil {
		log.Printf("saving state: %v", err)
	}
}

func (e *Engine) applyRules(s model.Sample, now time.Time) {
	for _, r := range e.rules {
		if r.cfg.Metric != s.Metric {
			continue
		}
		if r.cfg.OnChange {
			e.evalOnChange(r, s, now)
		} else {
			e.evalCondition(r, s, now)
		}
	}
}

func (e *Engine) evalOnChange(r *compiledRule, s model.Sample, now time.Time) {
	prev, existed := e.st.Metric(s.Metric)
	if !existed || prev.Value.Equal(s.Value) {
		return
	}
	e.dispatch(model.Event{
		Source: "rule/" + s.Metric,
		Level:  model.Level(r.cfg.Level),
		Title:  fmt.Sprintf("%s changed: %s → %s", s.Metric, prev.Value.String(), s.Value.String()),
		Time:   now,
		Fields: map[string]string{"previous": prev.Value.String(), "current": s.Value.String()},
	}, r.cfg.Notify)
}

func (e *Engine) evalCondition(r *compiledRule, s model.Sample, now time.Time) {
	rs := e.st.Rule(r.id)
	matched, err := config.EvalCondition(r.prog, s.Value.Native())
	if err != nil {
		log.Printf("rule %q on %s: %v", r.cfg.Condition, s.Metric, err)
		return
	}

	if matched {
		rs.ResolveSince = nil
		if rs.Firing {
			return
		}
		if rs.PendingSince == nil {
			t := now
			rs.PendingSince = &t
		}
		if now.Sub(*rs.PendingSince) >= r.cfg.For.Duration {
			rs.Firing = true
			rs.PendingSince = nil
			cooldown := e.cooldownFor(r.cfg)
			if rs.LastFire != nil && now.Sub(*rs.LastFire) < cooldown {
				rs.NotifiedFiring = false // suppressed by cooldown
				return
			}
			t := now
			rs.LastFire = &t
			rs.NotifiedFiring = true
			e.dispatch(model.Event{
				Source: "rule/" + s.Metric,
				Level:  model.Level(r.cfg.Level),
				Title:  fmt.Sprintf("%s: %s (value: %s)", s.Metric, r.cfg.Condition, s.Value.String()),
				Time:   now,
				Fields: map[string]string{"value": s.Value.String(), "condition": r.cfg.Condition},
			}, r.cfg.Notify)
		}
		return
	}

	// condition false
	rs.PendingSince = nil
	if !rs.Firing {
		return
	}
	if rs.ResolveSince == nil {
		t := now
		rs.ResolveSince = &t
	}
	if now.Sub(*rs.ResolveSince) >= r.cfg.ResolveFor.Duration {
		rs.Firing = false
		rs.ResolveSince = nil
		if rs.NotifiedFiring {
			e.dispatch(model.Event{
				Source:   "rule/" + s.Metric,
				Level:    model.LevelInfo,
				Title:    fmt.Sprintf("%s: resolved (value: %s)", s.Metric, s.Value.String()),
				Time:     now,
				Resolved: true,
			}, r.cfg.Notify)
		}
		rs.NotifiedFiring = false
	}
}

func (e *Engine) fireOnChangeForAbsent(metric string, prev *state.MetricState, now time.Time) {
	for _, r := range e.rules {
		if !r.cfg.OnChange || r.cfg.Metric != metric {
			continue
		}
		prevStr := ""
		if prev != nil {
			prevStr = prev.Value.String()
		}
		e.dispatch(model.Event{
			Source: "rule/" + metric,
			Level:  model.Level(r.cfg.Level),
			Title:  fmt.Sprintf("%s disappeared (was: %s)", metric, prevStr),
			Time:   now,
		}, r.cfg.Notify)
	}
}

func (e *Engine) cooldownFor(r *config.Rule) time.Duration {
	if r != nil && r.Cooldown != nil {
		return r.Cooldown.Duration
	}
	return e.cfg.Defaults.Cooldown.Duration
}

func (e *Engine) dispatch(ev model.Event, targets []string) {
	for _, target := range targets {
		if err := e.queue.Enqueue(target, ev); err != nil {
			log.Printf("enqueue to %s: %v", target, err)
		}
	}
}

func dedupKey(e model.Event) string {
	h := sha256.Sum256([]byte(e.Source + "\x00" + string(e.Level) + "\x00" + e.Title + "\x00" + e.Message))
	return hex.EncodeToString(h[:8])
}

// Sources lists the active source names (for run startup logging).
func (e *Engine) Sources() []string {
	var out []string
	for name := range e.sources {
		out = append(out, name)
	}
	return out
}
