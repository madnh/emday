package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

func testEvent() model.Event {
	return model.Event{
		Source:  "exec/backup",
		Level:   model.LevelError,
		Title:   "backup failed",
		Message: "disk full",
		Time:    time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	}
}

func capture(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	return server
}

// Lark signature must be deterministic and secret-dependent.
func TestLarkSignDeterministic(t *testing.T) {
	a, b := larkSign("secret", 1752400000), larkSign("secret", 1752400000)
	if a != b || a == "" {
		t.Fatalf("larkSign not deterministic: %q vs %q", a, b)
	}
	if larkSign("other", 1752400000) == a {
		t.Fatal("different secrets must sign differently")
	}
}

// Lark answers HTTP 200 even on errors — the body's code field is the truth.
func TestLarkBodyError(t *testing.T) {
	server := capture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"code": 19021, "msg": "sign match fail"}`)
	})
	l, _ := newLark("l", &config.Notifier{Type: "lark", URL: server.URL})
	err := l.Send(context.Background(), testEvent())
	if err == nil || !strings.Contains(err.Error(), "19021") {
		t.Fatalf("expected lark body error, got %v", err)
	}
}

func TestLarkCardShape(t *testing.T) {
	var got map[string]any
	server := capture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"code": 0}`)
	})
	l, _ := newLark("l", &config.Notifier{Type: "lark", URL: server.URL, Secret: "s3cret"})
	if err := l.Send(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}
	if got["msg_type"] != "interactive" {
		t.Errorf("msg_type = %v", got["msg_type"])
	}
	if got["sign"] == nil || got["timestamp"] == nil {
		t.Error("signed request must carry sign + timestamp")
	}
	card := got["card"].(map[string]any)
	header := card["header"].(map[string]any)
	if header["template"] != "red" {
		t.Errorf("error level should render a red header, got %v", header["template"])
	}
}

// Telegram HTML mode: untrusted script output must be escaped.
func TestTelegramEscapesUntrusted(t *testing.T) {
	var payload struct {
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	server := capture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&payload)
		io.WriteString(w, `{"ok": true}`)
	})
	tg, _ := newTelegram("t", &config.Notifier{Type: "telegram", Token: "tok", ChatID: "1"})
	tg.api = server.URL
	e := testEvent()
	e.Message = `<script>alert("pwn")</script>`
	if err := tg.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if payload.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q", payload.ParseMode)
	}
	if strings.Contains(payload.Text, "<script>") {
		t.Fatalf("unescaped untrusted detail: %s", payload.Text)
	}
	if !strings.Contains(payload.Text, "&lt;script&gt;") {
		t.Fatalf("expected escaped detail, got: %s", payload.Text)
	}
}

// ntfy renders Tags as a leading emoji — the title must stay emoji-free.
func TestNtfyHeaders(t *testing.T) {
	var title, tags, priority string
	server := capture(t, func(w http.ResponseWriter, r *http.Request) {
		title, tags, priority = r.Header.Get("Title"), r.Header.Get("Tags"), r.Header.Get("Priority")
	})
	n, _ := newNtfy("n", &config.Notifier{Type: "ntfy", URL: server.URL})
	if err := n.Send(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}
	if title != "backup failed" {
		t.Errorf("title = %q (must be plain, no emoji)", title)
	}
	if tags != "rotating_light" || priority != "urgent" {
		t.Errorf("tags/priority = %q/%q", tags, priority)
	}
}

func TestSlackAndDiscordShape(t *testing.T) {
	var slackGot, discordGot map[string]any
	slackSrv := capture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&slackGot)
	})
	discordSrv := capture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&discordGot)
	})

	s, _ := newSlack("s", &config.Notifier{Type: "slack", URL: slackSrv.URL})
	d, _ := newDiscord("d", &config.Notifier{Type: "discord", URL: discordSrv.URL})
	if err := s.Send(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}
	if err := d.Send(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}

	att := slackGot["attachments"].([]any)[0].(map[string]any)
	if att["color"] != "#d40e0d" {
		t.Errorf("slack error color = %v", att["color"])
	}
	emb := discordGot["embeds"].([]any)[0].(map[string]any)
	if int(emb["color"].(float64)) != 0xd40e0d {
		t.Errorf("discord error color = %v", emb["color"])
	}
}
