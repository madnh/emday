package source

import (
	"strings"
	"testing"
	"time"

	"github.com/madnh/emday/internal/model"
)

func TestParseOutputMetricsAndNotify(t *testing.T) {
	payload := strings.Join([]string{
		"BACKUP_STATUS=failed",
		"BACKUP_SIZE_GB=42.5",
		"",
		"DETAIL<<END",
		"line 1",
		"line 2",
		"END",
		"NOTIFY_ERROR<<X",
		"Backup failed on db01",
		"disk full (98%)",
		"X",
		"NOTIFY=all good otherwise",
		"garbage line without equals",
	}, "\n")

	samples, events, warns := parseOutput("backup", payload, time.Now(), false)

	byName := map[string]model.Value{}
	for _, s := range samples {
		byName[s.Metric] = s.Value
	}
	if v := byName["backup.BACKUP_STATUS"]; v.IsNum || v.Str != "failed" {
		t.Errorf("BACKUP_STATUS = %+v", v)
	}
	if v := byName["backup.BACKUP_SIZE_GB"]; !v.IsNum || v.Num != 42.5 {
		t.Errorf("BACKUP_SIZE_GB = %+v", v)
	}
	if v := byName["backup.DETAIL"]; v.Str != "line 1\nline 2" {
		t.Errorf("DETAIL = %q", v.Str)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Level != model.LevelError || events[0].Title != "Backup failed on db01" || events[0].Message != "disk full (98%)" {
		t.Errorf("error event = %+v", events[0])
	}
	if events[1].Level != model.LevelInfo || events[1].Title != "all good otherwise" {
		t.Errorf("info event = %+v", events[1])
	}
	if events[0].Source != "exec/backup" {
		t.Errorf("source = %q", events[0].Source)
	}

	if len(warns) != 1 || !strings.Contains(warns[0], "malformed") {
		t.Errorf("warns = %v", warns)
	}
}

func TestParseOutputStdoutModeRejectsNotify(t *testing.T) {
	payload := "OK=1\nNOTIFY_ERROR=injected!"
	samples, events, warns := parseOutput("s", payload, time.Now(), true)
	if len(events) != 0 {
		t.Fatalf("stdout mode must not emit events, got %+v", events)
	}
	if len(samples) != 1 || samples[0].Metric != "s.OK" {
		t.Errorf("samples = %+v", samples)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "NOTIFY_ERROR") {
		t.Errorf("expected a warning about the ignored directive, got %v", warns)
	}
}

func TestParseOutputUnterminatedHeredoc(t *testing.T) {
	_, _, warns := parseOutput("s", "KEY<<END\nno terminator", time.Now(), false)
	if len(warns) != 1 || !strings.Contains(warns[0], "never terminated") {
		t.Errorf("warns = %v", warns)
	}
}

func TestValidateIP(t *testing.T) {
	cases := []struct {
		raw, family string
		want        string
		ok          bool
	}{
		{"203.0.113.7", "v4", "203.0.113.7", true},
		{"203.0.113.7\n", "v4", "", false}, // caller trims; raw newline inside is rejected
		{"2001:db8::1", "v6", "2001:db8::1", true},
		{"2001:db8::1", "v4", "", false},       // wrong family
		{"203.0.113.7", "v6", "", false},       // wrong family
		{"<html>oops</html>", "v4", "", false}, // junk
		{"203.0.113.7 extra", "v4", "", false}, // extra tokens
		{"", "v4", "", false},
	}
	for _, c := range cases {
		got, err := ValidateIP(c.raw, c.family)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("ValidateIP(%q, %s) = %q, %v; want %q", c.raw, c.family, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateIP(%q, %s) should fail", c.raw, c.family)
		}
	}
}
