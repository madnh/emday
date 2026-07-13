package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
	"github.com/madnh/emday/internal/notify"
	"github.com/madnh/emday/internal/state"
)

// received collects webhook deliveries from the test server.
type received struct {
	mu     sync.Mutex
	events []map[string]any
}

func (r *received) titles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		out = append(out, e["title"].(string))
	}
	return out
}

// newTestEngine wires a real engine to an httptest webhook.
func newTestEngine(t *testing.T, rules []*config.Rule, srcNotify []string) (*Engine, *received, func()) {
	t.Helper()
	rec := &received{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		rec.mu.Lock()
		rec.events = append(rec.events, payload)
		rec.mu.Unlock()
	}))

	dir := t.TempDir()
	cfg := &config.Config{
		Version: 1,
		Sources: map[string]*config.Source{
			"test": {Type: "exec", Command: "true", Notify: srcNotify},
		},
		Rules: rules,
		Notifiers: map[string]*config.Notifier{
			"hook": {Type: "webhook", URL: server.URL},
		},
		Dir: dir,
	}
	cfg.Defaults.Cooldown.Duration = 30 * time.Minute

	st, err := state.Load(cfg.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	notifiers := map[string]notify.Notifier{}
	for name, nc := range cfg.Notifiers {
		n, err := notify.New(name, nc)
		if err != nil {
			t.Fatal(err)
		}
		notifiers[name] = n
	}
	queue, err := notify.NewQueue(cfg.QueueDir(), notifiers)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := New(cfg, st, queue)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { queue.Run(ctx); close(done) }()
	cleanup := func() {
		cancel()
		<-done
		server.Close()
	}
	return eng, rec, cleanup
}

func sample(metric string, v model.Value) []model.Sample {
	return []model.Sample{{Metric: metric, Value: v, Time: time.Now()}}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestOnChangeRule(t *testing.T) {
	rules := []*config.Rule{{Metric: "test.IP", OnChange: true, Level: "info", Notify: []string{"hook"}}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	eng.Process("test", sample("test.IP", model.StrValue("1.1.1.1")), nil)
	eng.Process("test", sample("test.IP", model.StrValue("1.1.1.1")), nil) // no change
	eng.Process("test", sample("test.IP", model.StrValue("2.2.2.2")), nil) // change!

	waitFor(t, func() bool { return len(rec.titles()) >= 1 })
	titles := rec.titles()
	if len(titles) != 1 || titles[0] != "test.IP changed: 1.1.1.1 → 2.2.2.2" {
		t.Errorf("titles = %v", titles)
	}
}

func TestConditionForAndResolve(t *testing.T) {
	forDur := config.Duration{}
	forDur.Duration = 50 * time.Millisecond
	rules := []*config.Rule{{
		Metric: "test.cpu", Condition: "value >= 90", For: forDur,
		Level: "warn", Notify: []string{"hook"},
	}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	hot := sample("test.cpu", model.NumValue(95))
	eng.Process("test", hot, nil) // starts pending, no alert yet
	if len(rec.titles()) != 0 {
		t.Fatalf("alert before `for` elapsed: %v", rec.titles())
	}
	time.Sleep(60 * time.Millisecond)
	eng.Process("test", hot, nil) // for elapsed → fire

	waitFor(t, func() bool { return len(rec.titles()) == 1 })

	eng.Process("test", sample("test.cpu", model.NumValue(50)), nil) // resolve (resolve_for=0)
	waitFor(t, func() bool { return len(rec.titles()) == 2 })

	titles := rec.titles()
	if titles[0] != "test.cpu: value >= 90 (value: 95)" {
		t.Errorf("fire title = %q", titles[0])
	}
	if titles[1] != "test.cpu: resolved (value: 50)" {
		t.Errorf("resolve title = %q", titles[1])
	}
}

func TestConditionFlappingResetsForTimer(t *testing.T) {
	forDur := config.Duration{}
	forDur.Duration = 80 * time.Millisecond
	rules := []*config.Rule{{
		Metric: "test.cpu", Condition: "value >= 90", For: forDur,
		Level: "warn", Notify: []string{"hook"},
	}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	eng.Process("test", sample("test.cpu", model.NumValue(95)), nil)
	time.Sleep(50 * time.Millisecond)
	eng.Process("test", sample("test.cpu", model.NumValue(10)), nil) // dips → reset
	time.Sleep(50 * time.Millisecond)
	eng.Process("test", sample("test.cpu", model.NumValue(95)), nil) // pending restarts

	if n := len(rec.titles()); n != 0 {
		t.Errorf("flapping fired %d alert(s): %v", n, rec.titles())
	}
}

func TestDirectEventsDedup(t *testing.T) {
	eng, rec, cleanup := newTestEngine(t, nil, []string{"hook"})
	defer cleanup()

	ev := model.Event{Source: "exec/test", Level: model.LevelError, Title: "backup failed", Time: time.Now()}
	eng.Process("test", nil, []model.Event{ev})
	eng.Process("test", nil, []model.Event{ev}) // identical within cooldown → suppressed
	other := ev
	other.Title = "different problem"
	eng.Process("test", nil, []model.Event{other})

	waitFor(t, func() bool { return len(rec.titles()) >= 2 })
	time.Sleep(100 * time.Millisecond) // give a wrong extra delivery time to appear
	titles := rec.titles()
	if len(titles) != 2 {
		t.Errorf("dedup failed, deliveries: %v", titles)
	}
}
