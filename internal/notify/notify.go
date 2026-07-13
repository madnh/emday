// Package notify implements the notification providers and the persistent
// per-notifier queue (so alerts survive network outages and restarts).
package notify

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// Notifier delivers one event. Send errors are retried by the queue.
type Notifier interface {
	Name() string
	Send(ctx context.Context, e model.Event) error
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// New builds a notifier from config.
func New(name string, cfg *config.Notifier) (Notifier, error) {
	switch cfg.Type {
	case "webhook":
		return newWebhook(name, cfg)
	case "telegram":
		return newTelegram(name, cfg)
	case "ntfy":
		return newNtfy(name, cfg)
	case "lark":
		return newLark(name, cfg)
	case "slack":
		return newSlack(name, cfg)
	case "discord":
		return newDiscord(name, cfg)
	default:
		return nil, fmt.Errorf("unknown notifier type %q", cfg.Type)
	}
}

// RenderText is the default human-readable rendering shared by providers.
func RenderText(e model.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", levelIcon(e), e.Title)
	if e.Message != "" {
		fmt.Fprintf(&b, "\n%s", e.Message)
	}
	fmt.Fprintf(&b, "\n— %s · %s · %s", hostname(), e.Source, e.Time.Format("2006-01-02 15:04:05"))
	return b.String()
}

// secret resolves the telegram token: prefer the env var over the inline value.
func secret(cfg *config.Notifier) string {
	if cfg.TokenEnv != "" {
		return os.Getenv(cfg.TokenEnv)
	}
	return cfg.Token
}

// signSecret resolves lark's optional signing secret the same way.
func signSecret(cfg *config.Notifier) string {
	if cfg.SecretEnv != "" {
		return os.Getenv(cfg.SecretEnv)
	}
	return cfg.Secret
}
