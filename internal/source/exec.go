package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// EnvOutputFile is the env var handed to exec scripts, pointing at the file
// they append KEY=VALUE pairs and NOTIFY_* directives to (GitHub Actions'
// $GITHUB_OUTPUT model).
const EnvOutputFile = "EMDAY_OUTPUT"

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type execSource struct {
	name    string
	command string
	timeout time.Duration
	stdout  bool // parse: stdout mode (metrics only, no NOTIFY_*)
	tmpDir  string
}

func newExecSource(name string, cfg *config.Source, tmpDir string) *execSource {
	return &execSource{
		name:    name,
		command: cfg.Command,
		timeout: cfg.Timeout.Duration,
		stdout:  cfg.Parse == "stdout",
		tmpDir:  tmpDir,
	}
}

func (s *execSource) Name() string { return s.name }

func (s *execSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var outFile string
	if !s.stdout {
		if err := os.MkdirAll(s.tmpDir, 0o700); err != nil {
			return nil, nil, err
		}
		f, err := os.CreateTemp(s.tmpDir, "out-"+s.name+"-*")
		if err != nil {
			return nil, nil, err
		}
		outFile = f.Name()
		f.Close()
		defer os.Remove(outFile)
	}

	cmd := shellCommand(ctx, s.command)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "EMDAY_SOURCE="+s.name)
	if outFile != "" {
		cmd.Env = append(cmd.Env, EnvOutputFile+"="+outFile)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	elapsed := time.Since(start)
	now := time.Now()

	exitCode := 0
	if runErr != nil {
		exitCode = -1
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}

	var payload string
	if s.stdout {
		payload = stdout.String()
	} else {
		raw, err := os.ReadFile(outFile)
		if err == nil {
			payload = string(raw)
		}
	}

	samples, events, parseWarns := parseOutput(s.name, payload, now, s.stdout)

	// Source health is itself data: rules can watch `<name>._ok`.
	samples = append(samples,
		model.Sample{Metric: s.name + "._ok", Value: model.BoolValue(exitCode == 0), Time: now},
		model.Sample{Metric: s.name + "._exit_code", Value: model.NumValue(float64(exitCode)), Time: now},
		model.Sample{Metric: s.name + "._duration_ms", Value: model.NumValue(float64(elapsed.Milliseconds())), Time: now},
	)

	var err error
	if runErr != nil {
		err = fmt.Errorf("command failed (exit %d): %v; stderr: %.500s", exitCode, runErr, stderr.String())
	} else if len(parseWarns) > 0 {
		err = fmt.Errorf("output issues: %s", strings.Join(parseWarns, "; "))
	}
	return samples, events, err
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// parseOutput reads KEY=VALUE lines, KEY<<DELIM heredocs, and NOTIFY_*
// directives. In stdout mode NOTIFY_* is rejected (injection guard: tools
// the command calls may print arbitrary lines).
func parseOutput(source, payload string, now time.Time, stdoutMode bool) ([]model.Sample, []model.Event, []string) {
	var samples []model.Sample
	var events []model.Event
	var warns []string

	lines := strings.Split(payload, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		var key, value string
		if k, delim, ok := parseHeredocHeader(line); ok {
			var body []string
			terminated := false
			for i++; i < len(lines); i++ {
				l := strings.TrimRight(lines[i], "\r")
				if l == delim {
					terminated = true
					break
				}
				body = append(body, l)
			}
			if !terminated {
				warns = append(warns, fmt.Sprintf("heredoc for %s never terminated by %q", k, delim))
				continue
			}
			key, value = k, strings.Join(body, "\n")
		} else if k, v, ok := strings.Cut(line, "="); ok && keyRe.MatchString(k) {
			key, value = k, v
		} else {
			warns = append(warns, fmt.Sprintf("ignored malformed line %.80q", line))
			continue
		}

		if level, isNotify := notifyLevel(key); isNotify {
			if stdoutMode {
				warns = append(warns, fmt.Sprintf("%s directive ignored in parse:stdout mode (use the $%s file channel)", key, EnvOutputFile))
				continue
			}
			title, message, _ := strings.Cut(value, "\n")
			events = append(events, model.Event{
				Source:  "exec/" + source,
				Level:   level,
				Title:   strings.TrimSpace(title),
				Message: strings.TrimSpace(message),
				Time:    now,
			})
			continue
		}

		samples = append(samples, model.Sample{
			Metric: source + "." + key,
			Value:  model.ParseValue(value),
			Time:   now,
		})
	}
	return samples, events, warns
}

// parseHeredocHeader matches `KEY<<DELIM`.
func parseHeredocHeader(line string) (key, delim string, ok bool) {
	k, d, found := strings.Cut(line, "<<")
	if !found || !keyRe.MatchString(k) || strings.TrimSpace(d) == "" || strings.Contains(d, "=") {
		return "", "", false
	}
	return k, strings.TrimSpace(d), true
}

func notifyLevel(key string) (model.Level, bool) {
	switch key {
	case "NOTIFY", "NOTIFY_INFO":
		return model.LevelInfo, true
	case "NOTIFY_WARN":
		return model.LevelWarn, true
	case "NOTIFY_ERROR":
		return model.LevelError, true
	}
	return "", false
}

// cleanupTmp removes stale output files from crashed runs (best effort).
func CleanupTmp(tmpDir string, olderThan time.Duration) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-olderThan)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(tmpDir, e.Name()))
		}
	}
}
