package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// TestQueuePersistsAcrossOutage: an event enqueued while the target is down
// stays on disk and is delivered once the target comes back.
func TestQueuePersistsAcrossOutage(t *testing.T) {
	oldMin, oldMax := retryMin, retryMax
	retryMin, retryMax = 20*time.Millisecond, 50*time.Millisecond
	defer func() { retryMin, retryMax = oldMin, oldMax }()

	var mu sync.Mutex
	up := false
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !up {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		got = append(got, payload["title"].(string))
	}))
	defer server.Close()

	queueDir := filepath.Join(t.TempDir(), "queue")
	n, err := New("hook", &config.Notifier{Type: "webhook", URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := NewQueue(queueDir, map[string]Notifier{"hook": n})
	if err != nil {
		t.Fatal(err)
	}

	ev := model.Event{Source: "test", Level: model.LevelInfo, Title: "queued while down", Time: time.Now()}
	if err := queue.Enqueue("hook", ev); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(queueDir, "hook"))
	if len(entries) != 1 {
		t.Fatalf("queued files = %d, want 1", len(entries))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	time.Sleep(50 * time.Millisecond) // let the first attempts fail

	mu.Lock()
	up = true
	mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		delivered := len(got)
		mu.Unlock()
		entries, _ := os.ReadDir(filepath.Join(queueDir, "hook"))
		if delivered == 1 && len(entries) == 0 {
			return // delivered and dequeued
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("event was not delivered after the target came back")
}
