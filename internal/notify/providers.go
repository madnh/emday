package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// templateData is the environment available to webhook body templates.
type templateData struct {
	Level    string
	Title    string
	Message  string
	Source   string
	Time     string
	Resolved bool
	Hostname string
	Fields   map[string]string
	Text     string // the default rendering, for templates that just wrap it
}

func newTemplateData(e model.Event) templateData {
	host, _ := os.Hostname()
	return templateData{
		Level:    string(e.Level),
		Title:    e.Title,
		Message:  e.Message,
		Source:   e.Source,
		Time:     e.Time.Format(time.RFC3339),
		Resolved: e.Resolved,
		Hostname: host,
		Fields:   e.Fields,
		Text:     RenderText(e),
	}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// --- webhook ---

type webhook struct {
	name    string
	url     string
	method  string
	headers map[string]string
	tmpl    *template.Template
}

func newWebhook(name string, cfg *config.Notifier) (*webhook, error) {
	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}
	body := cfg.BodyTemplate
	if body == "" {
		body = `{"level": {{json .Level}}, "title": {{json .Title}}, "message": {{json .Message}}, "fields": {{json .Fields}}, "source": {{json .Source}}, "host": {{json .Hostname}}, "time": {{json .Time}}, "resolved": {{.Resolved}}}`
	}
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
	}).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("body_template: %w", err)
	}
	url, err := resolveURL(cfg)
	if err != nil {
		return nil, err
	}
	return &webhook{name: name, url: url, method: method, headers: cfg.Headers, tmpl: tmpl}, nil
}

func (w *webhook) Name() string { return w.name }

func (w *webhook) Send(ctx context.Context, e model.Event) error {
	var body bytes.Buffer
	if err := w.tmpl.Execute(&body, newTemplateData(e)); err != nil {
		return fmt.Errorf("render body_template: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, w.method, w.url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	_, err = doHTTP(req)
	return err
}

// --- telegram (bot API, HTML parse mode) ---

type telegram struct {
	name string
	cfg  *config.Notifier
	api  string // override in tests; empty = api.telegram.org
}

func newTelegram(name string, cfg *config.Notifier) (*telegram, error) {
	return &telegram{name: name, cfg: cfg}, nil
}

func (t *telegram) Name() string { return t.name }

func (t *telegram) Send(ctx context.Context, e model.Event) error {
	token := secret(t.cfg)
	if token == "" {
		return fmt.Errorf("telegram token is empty (env %s not set?)", t.cfg.TokenEnv)
	}
	// Title/Message may carry untrusted data (exec script output) — escape
	// for HTML parse mode.
	text := fmt.Sprintf("<b>%s %s</b>", levelIcon(e), html.EscapeString(e.Title))
	if e.Message != "" {
		text += "\n" + html.EscapeString(e.Message)
	}
	for _, k := range sortedFieldKeys(e.Fields) {
		text += fmt.Sprintf("\n%s: <code>%s</code>", html.EscapeString(k), html.EscapeString(e.Fields[k]))
	}
	text += fmt.Sprintf("\n<code>%s · %s · %s</code>",
		html.EscapeString(hostname()), html.EscapeString(e.Source), e.Time.Format("2006-01-02 15:04:05"))
	payload, _ := json.Marshal(map[string]any{
		"chat_id":                  t.cfg.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	api := t.api
	if api == "" {
		api = "https://api.telegram.org"
	}
	return postJSON(ctx, api+"/bot"+token+"/sendMessage", payload)
}

// --- ntfy ---

type ntfy struct {
	name     string
	url      string
	priority string
}

func newNtfy(name string, cfg *config.Notifier) (*ntfy, error) {
	url, err := resolveURL(cfg)
	if err != nil {
		return nil, err
	}
	return &ntfy{name: name, url: url, priority: cfg.Priority}, nil
}

func (n *ntfy) Name() string { return n.name }

func (n *ntfy) Send(ctx context.Context, e model.Event) error {
	body := e.Message
	for _, k := range sortedFieldKeys(e.Fields) {
		if body != "" {
			body += "\n"
		}
		body += k + ": " + e.Fields[k]
	}
	if body == "" {
		body = e.Title
	}
	body += "\n— " + hostname() + " · " + e.Source
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, strings.NewReader(body))
	if err != nil {
		return err
	}
	// ntfy renders Tags as a leading emoji — keep the title plain so the
	// icon is not doubled.
	req.Header.Set("Title", e.Title)
	req.Header.Set("Tags", ntfyTag(e))
	priority := n.priority
	if priority == "" {
		priority = map[model.Level]string{
			model.LevelInfo:  "default",
			model.LevelWarn:  "high",
			model.LevelError: "urgent",
		}[e.Level]
	}
	if e.Resolved {
		priority = "default"
	}
	req.Header.Set("Priority", priority)
	_, err = doHTTP(req)
	return err
}

func ntfyTag(e model.Event) string {
	if e.Resolved {
		return "white_check_mark"
	}
	switch e.Level {
	case model.LevelWarn:
		return "warning"
	case model.LevelError:
		return "rotating_light"
	default:
		return "information_source"
	}
}

// --- lark / feishu (custom bot webhook, interactive card) ---

type lark struct {
	name string
	url  string
	cfg  *config.Notifier
}

func newLark(name string, cfg *config.Notifier) (*lark, error) {
	url, err := resolveURL(cfg)
	if err != nil {
		return nil, err
	}
	return &lark{name: name, url: url, cfg: cfg}, nil
}

func (l *lark) Name() string { return l.name }

// larkSign implements the custom-bot signature: the string-to-sign
// "timestamp\nsecret" is the HMAC key over an empty message.
func larkSign(secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(fmt.Sprintf("%d\n%s", ts, secret)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (l *lark) Send(ctx context.Context, e model.Event) error {
	template := map[model.Level]string{
		model.LevelInfo:  "blue",
		model.LevelWarn:  "orange",
		model.LevelError: "red",
	}[e.Level]
	if e.Resolved {
		template = "green"
	}
	fields := []any{
		larkField(true, "**Host**\n"+hostname()),
		larkField(true, "**Source**\n"+e.Source),
	}
	for _, k := range sortedFieldKeys(e.Fields) {
		fields = append(fields, larkField(true, "**"+k+"**\n"+e.Fields[k]))
	}
	if e.Message != "" {
		fields = append(fields, larkField(false, "**Detail**\n"+e.Message))
	}
	payload := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": e.Title},
				"template": template,
			},
			"elements": []any{
				map[string]any{"tag": "div", "fields": fields},
				map[string]any{"tag": "note", "elements": []any{
					map[string]any{"tag": "plain_text", "content": e.Time.Format("2006-01-02 15:04:05 MST") + " · emday"},
				}},
			},
		},
	}
	// A configured-but-empty secret means the env var is missing where this
	// process runs — fail with the real cause instead of letting Lark answer
	// an opaque "sign match fail" (code 19021).
	if l.cfg.Secret != "" || l.cfg.SecretEnv != "" {
		s := signSecret(l.cfg)
		if s == "" {
			return fmt.Errorf("lark signing secret is empty (env %s not set in this environment? the service does not inherit your shell)", l.cfg.SecretEnv)
		}
		ts := time.Now().Unix()
		payload["timestamp"] = fmt.Sprintf("%d", ts)
		payload["sign"] = larkSign(s, ts)
	}
	b, _ := json.Marshal(payload)

	// Lark answers HTTP 200 even on errors; the real status is in the body.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := doHTTP(req)
	if err != nil {
		return err
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &r); err == nil && r.Code != 0 {
		return fmt.Errorf("lark code %d: %s", r.Code, r.Msg)
	}
	return nil
}

func larkField(short bool, md string) map[string]any {
	return map[string]any{
		"is_short": short,
		"text":     map[string]any{"tag": "lark_md", "content": md},
	}
}

// --- slack (incoming webhook) ---

type slack struct {
	name string
	url  string
}

func newSlack(name string, cfg *config.Notifier) (*slack, error) {
	url, err := resolveURL(cfg)
	if err != nil {
		return nil, err
	}
	return &slack{name: name, url: url}, nil
}

func (s *slack) Name() string { return s.name }

func (s *slack) Send(ctx context.Context, e model.Event) error {
	payload, _ := json.Marshal(map[string]any{
		"attachments": []map[string]any{{
			"color":  levelColorHex(e),
			"title":  levelIcon(e) + " " + e.Title,
			"text":   fmt.Sprintf("%s\n`%s` · `%s`", e.Message, hostname(), e.Source),
			"footer": "emday",
			"ts":     e.Time.Unix(),
		}},
	})
	return postJSON(ctx, s.url, payload)
}

// --- discord (webhook) ---

type discord struct {
	name string
	url  string
}

func newDiscord(name string, cfg *config.Notifier) (*discord, error) {
	url, err := resolveURL(cfg)
	if err != nil {
		return nil, err
	}
	return &discord{name: name, url: url}, nil
}

func (d *discord) Name() string { return d.name }

func (d *discord) Send(ctx context.Context, e model.Event) error {
	color := map[model.Level]int{
		model.LevelInfo:  0x439fe0,
		model.LevelWarn:  0xdaa038,
		model.LevelError: 0xd40e0d,
	}[e.Level]
	if e.Resolved {
		color = 0x2eb886
	}
	payload, _ := json.Marshal(map[string]any{
		"embeds": []any{map[string]any{
			"title":       levelIcon(e) + " " + e.Title,
			"description": fmt.Sprintf("%s\n`%s` · `%s`", e.Message, hostname(), e.Source),
			"color":       color,
			"timestamp":   e.Time.UTC().Format(time.RFC3339),
			"footer":      map[string]any{"text": "emday"},
		}},
	})
	return postJSON(ctx, d.url, payload)
}

// --- shared helpers ---

func levelIcon(e model.Event) string {
	if e.Resolved {
		return "✅"
	}
	switch e.Level {
	case model.LevelWarn:
		return "⚠️"
	case model.LevelError:
		return "🔴"
	default:
		return "ℹ️"
	}
}

func levelColorHex(e model.Event) string {
	if e.Resolved {
		return "#2eb886"
	}
	switch e.Level {
	case model.LevelWarn:
		return "#daa038"
	case model.LevelError:
		return "#d40e0d"
	default:
		return "#439fe0"
	}
}

func postJSON(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = doHTTP(req)
	return err
}

// doHTTP sends the request and returns the response body; non-2xx is an
// error carrying a body snippet.
func doHTTP(req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %.300s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	return body, nil
}
