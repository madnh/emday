package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if len(titles) != 1 || titles[0] != "test.IP changed" {
		t.Errorf("titles = %v", titles)
	}
	// the values travel as fields, not in the title
	rec.mu.Lock()
	fields, _ := rec.events[0]["fields"].(map[string]any)
	rec.mu.Unlock()
	if fields["from"] != "1.1.1.1" || fields["to"] != "2.2.2.2" {
		t.Errorf("fields = %v", fields)
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
	if titles[0] != "test.cpu: value >= 90" {
		t.Errorf("fire title = %q", titles[0])
	}
	if titles[1] != "test.cpu: resolved" {
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

// The success path used to be entirely silent: a rule could fire, enqueue and
// deliver without writing one line, so an operator reading the journal could
// not tell "nothing fired" from "fired and was delivered".
func TestFiringAndDeliveryAreLogged(t *testing.T) {
	var buf lockedBuffer
	restore := captureLog(&buf)
	defer restore()

	rules := []*config.Rule{{Metric: "test.cpu", Condition: "value >= 90", Level: "error", Notify: []string{"hook"}}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	eng.Process("test", sample("test.cpu", model.NumValue(95)), nil)
	waitFor(t, func() bool { return len(rec.titles()) >= 1 })
	waitFor(t, func() bool { return strings.Contains(buf.String(), "delivered") })

	logged := buf.String()
	for _, want := range []string{
		"alert rule/test.cpu [error]", // the rule fired
		"(value=95)",                  // with the value that fired it
		"-> hook",                     // routed to this notifier
		"notifier hook: delivered",    // and actually sent
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("missing %q in log:\n%s", want, logged)
		}
	}
}

// A rule silenced by its cooldown must say so; otherwise a missing alert and a
// suppressed one look identical.
func TestCooldownSuppressionIsLogged(t *testing.T) {
	var buf lockedBuffer
	restore := captureLog(&buf)
	defer restore()

	rules := []*config.Rule{{Metric: "test.cpu", Condition: "value >= 90", Level: "warn", Notify: []string{"hook"}}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	eng.Process("test", sample("test.cpu", model.NumValue(95)), nil)
	waitFor(t, func() bool { return len(rec.titles()) >= 1 })
	eng.Process("test", sample("test.cpu", model.NumValue(50)), nil) // resolves
	eng.Process("test", sample("test.cpu", model.NumValue(97)), nil) // fires again, inside cooldown

	if !strings.Contains(buf.String(), "within its 30m0s cooldown, not notifying") {
		t.Errorf("cooldown suppression not logged:\n%s", buf.String())
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog redirects the standard logger and returns a restore func. The
// buffer is locked because the queue writes from its own goroutine.
func captureLog(w *lockedBuffer) func() {
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(w)
	log.SetFlags(0)
	return func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}
}

// A metric that stops being reported must NOT resolve a firing threshold rule:
// "certificate expiring" does not become fine because the host stopped
// answering. The resolved event belongs to the moment the value is measured
// again and is good.
func TestAbsentMetricNeitherResolvesNorRefiresAThresholdRule(t *testing.T) {
	rules := []*config.Rule{{Metric: "test.days_left", Condition: "value <= 7", Level: "error", Notify: []string{"hook"}}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	both := []model.Sample{
		{Metric: "test.days_left", Value: model.NumValue(3), Time: time.Now()},
		{Metric: "test.status", Value: model.StrValue("ok"), Time: time.Now()},
	}
	eng.Process("test", both, nil)
	waitFor(t, func() bool { return len(rec.titles()) >= 1 })

	// The probe now fails: days_left is gone, only status remains.
	eng.Process("test", sample("test.status", model.StrValue("dns-failure")), nil)
	eng.Process("test", sample("test.status", model.StrValue("dns-failure")), nil)
	time.Sleep(200 * time.Millisecond)
	if titles := rec.titles(); len(titles) != 1 {
		t.Fatalf("titles = %v, want only the original alert (no resolved while the metric is absent)", titles)
	}

	// Measured again, and healthy: now it resolves.
	eng.Process("test", []model.Sample{
		{Metric: "test.days_left", Value: model.NumValue(90), Time: time.Now()},
		{Metric: "test.status", Value: model.StrValue("ok"), Time: time.Now()},
	}, nil)
	waitFor(t, func() bool { return len(rec.titles()) >= 2 })
	if titles := rec.titles(); titles[1] != "test.days_left: resolved" {
		t.Errorf("titles = %v, want a resolved event once the value is measurable again", titles)
	}
}

// An on_change rule has nothing to compare the first observation against, so a
// rollout is silent — but a metric that DISAPPEARS is reported as a change.
func TestOnChangeIsSilentOnFirstObservationButReportsDisappearance(t *testing.T) {
	rules := []*config.Rule{{Metric: "test.issuer", OnChange: true, Level: "warn", Notify: []string{"hook"}}}
	eng, rec, cleanup := newTestEngine(t, rules, nil)
	defer cleanup()

	both := []model.Sample{
		{Metric: "test.issuer", Value: model.StrValue("Some CA"), Time: time.Now()},
		{Metric: "test.status", Value: model.StrValue("ok"), Time: time.Now()},
	}
	eng.Process("test", both, nil)
	time.Sleep(200 * time.Millisecond)
	if titles := rec.titles(); len(titles) != 0 {
		t.Fatalf("titles = %v, want nothing on the first observation", titles)
	}

	// The probe fails: issuer is no longer reported.
	eng.Process("test", sample("test.status", model.StrValue("dns-failure")), nil)
	waitFor(t, func() bool { return len(rec.titles()) >= 1 })
	if titles := rec.titles(); titles[0] != "test.issuer disappeared" {
		t.Errorf("titles = %v, want a disappearance event", titles)
	}

	// It comes back: with no stored previous value, that is a first
	// observation again, so it is silent.
	eng.Process("test", both, nil)
	time.Sleep(200 * time.Millisecond)
	if titles := rec.titles(); len(titles) != 1 {
		t.Errorf("titles = %v, want no second event when the metric reappears", titles)
	}
}
