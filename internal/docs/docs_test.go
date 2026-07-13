package docs

import (
	"strings"
	"testing"
)

// The CLI, README, and website all point at these topics by name — a missing
// or renamed topic breaks `emday docs <topic>` for users following docs.
func TestExpectedTopicsExist(t *testing.T) {
	for _, want := range []string{"index", "agent", "conditions", "config", "exec", "notifiers"} {
		content, err := Topic(want)
		if err != nil {
			t.Errorf("topic %q missing: %v", want, err)
			continue
		}
		if len(content) < 200 {
			t.Errorf("topic %q suspiciously short (%d bytes)", want, len(content))
		}
	}
}

func TestUnknownTopicListsAvailable(t *testing.T) {
	_, err := Topic("nonsense")
	if err == nil || !strings.Contains(err.Error(), "agent") {
		t.Fatalf("unknown-topic error must list available topics, got %v", err)
	}
}
