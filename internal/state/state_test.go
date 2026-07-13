package state

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/madnh/emday/internal/model"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Load(path) // missing file → fresh state
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Second)
	st.SetMetric("ip.public_v4", model.StrValue("1.2.3.4"), now)
	rs := st.Rule("cpu|value>90|5m0s")
	rs.Firing = true
	tt := now
	rs.LastFire = &tt

	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	st2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := st2.Metric("ip.public_v4")
	if !ok || m.Value.Str != "1.2.3.4" {
		t.Fatalf("metric lost across restart: %+v", m)
	}
	rs2 := st2.Rule("cpu|value>90|5m0s")
	if !rs2.Firing || rs2.LastFire == nil {
		t.Fatalf("rule state lost across restart: %+v", rs2)
	}
}

func TestCorruptFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt state must error with guidance, got %v", err)
	}
}

func TestNewerVersionRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte(`{"version": 99}`), 0o600)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "upgrade emday") {
		t.Fatalf("newer state version must be refused, got %v", err)
	}
}

func TestDedup(t *testing.T) {
	st, _ := Load(filepath.Join(t.TempDir(), "state.json"))
	t0 := time.Now()
	cooldown := 30 * time.Minute

	if !st.DedupAllows("k1", cooldown, t0) {
		t.Fatal("first occurrence must be allowed")
	}
	if st.DedupAllows("k1", cooldown, t0.Add(time.Minute)) {
		t.Fatal("identical event within cooldown must be suppressed")
	}
	if !st.DedupAllows("k1", cooldown, t0.Add(31*time.Minute)) {
		t.Fatal("after cooldown the event must fire again")
	}
	if !st.DedupAllows("k2", cooldown, t0) {
		t.Fatal("different key must not be affected")
	}
}

func TestMetricsWithPrefixAndDelete(t *testing.T) {
	st, _ := Load(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now()
	st.SetMetric("backup.STATUS", model.StrValue("ok"), now)
	st.SetMetric("backup.SIZE", model.NumValue(5), now)
	st.SetMetric("cpu.percent", model.NumValue(10), now)

	got := st.MetricsWithPrefix("backup.")
	sort.Strings(got)
	if len(got) != 2 || got[0] != "backup.SIZE" || got[1] != "backup.STATUS" {
		t.Fatalf("prefix scan = %v", got)
	}

	st.DeleteMetric("backup.STATUS")
	if _, ok := st.Metric("backup.STATUS"); ok {
		t.Fatal("deleted metric still present")
	}
}
