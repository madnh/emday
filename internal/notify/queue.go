package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madnh/emday/internal/model"
)

const (
	maxQueuedPerNotifier = 1000
	defaultRetryMin      = 15 * time.Second
	defaultRetryMax      = 5 * time.Minute
)

// Queue persists events per notifier and retries delivery with backoff, so
// "server lost internet, got it back" still delivers the queued alerts.
type Queue struct {
	dir       string
	notifiers map[string]Notifier
	seq       atomic.Uint64
	wake      chan struct{}
	wg        sync.WaitGroup

	// retry backoff bounds; set before Run starts (tests shrink them)
	retryMin time.Duration
	retryMax time.Duration
}

func NewQueue(dir string, notifiers map[string]Notifier) (*Queue, error) {
	q := &Queue{
		dir:       dir,
		notifiers: notifiers,
		wake:      make(chan struct{}, 1),
		retryMin:  defaultRetryMin,
		retryMax:  defaultRetryMax,
	}
	for name := range notifiers {
		if err := os.MkdirAll(q.notifierDir(name), 0o700); err != nil {
			return nil, err
		}
	}
	return q, nil
}

func (q *Queue) notifierDir(name string) string {
	return filepath.Join(q.dir, name)
}

// Enqueue persists the event for a notifier and wakes the dispatcher.
// Delivery order is by enqueue time (filename sorts chronologically).
func (q *Queue) Enqueue(notifier string, e model.Event) error {
	dir := q.notifierDir(notifier)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) >= maxQueuedPerNotifier {
		// Drop the oldest — losing the newest alert would be worse.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		os.Remove(filepath.Join(dir, entries[0].Name()))
		log.Printf("queue %s full (%d), dropped oldest event", notifier, maxQueuedPerNotifier)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%06d.json", time.Now().UnixNano(), q.seq.Add(1))
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		return err
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run drains queues until ctx is done. One worker per notifier so a slow
// target does not block the others.
func (q *Queue) Run(ctx context.Context) {
	for name, n := range q.notifiers {
		q.wg.Add(1)
		go func(name string, n Notifier) {
			defer q.wg.Done()
			q.drainLoop(ctx, name, n)
		}(name, n)
	}
	q.wg.Wait()
}

func (q *Queue) drainLoop(ctx context.Context, name string, n Notifier) {
	backoff := q.retryMin
	for {
		delivered, err := q.drainOnce(ctx, name, n)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("notifier %s: delivery failed, retrying in %s: %v", name, backoff, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, q.retryMax)
			continue
		}
		backoff = q.retryMin
		if delivered == 0 {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-time.After(30 * time.Second):
			}
		}
	}
}

// drainOnce sends queued events oldest-first, stopping at the first failure.
func (q *Queue) drainOnce(ctx context.Context, name string, n Notifier) (int, error) {
	dir := q.notifierDir(name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	sent := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			os.Remove(path)
			continue
		}
		var e model.Event
		if err := json.Unmarshal(raw, &e); err != nil {
			log.Printf("notifier %s: dropping corrupt queue entry %s: %v", name, entry.Name(), err)
			os.Remove(path)
			continue
		}
		if err := n.Send(ctx, e); err != nil {
			return sent, err
		}
		os.Remove(path)
		sent++
	}
	return sent, nil
}

// Pending counts queued events per notifier (used by doctor, read-only).
func Pending(queueDir string) map[string]int {
	out := map[string]int{}
	notifierDirs, err := os.ReadDir(queueDir)
	if err != nil {
		return out
	}
	for _, d := range notifierDirs {
		if !d.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(queueDir, d.Name()))
		if err == nil {
			out[d.Name()] = len(entries)
		}
	}
	return out
}
