// Package docs embeds emday's documentation so the binary is fully
// self-describing: humans and AI agents learn emday from emday itself
// (`emday docs <topic>`), no external documentation required.
package docs

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed topics/*.md
var topicsFS embed.FS

// Topics returns the available topic names, sorted.
func Topics() []string {
	entries, _ := topicsFS.ReadDir("topics")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

// Topic returns the content of one topic.
func Topic(name string) (string, error) {
	data, err := topicsFS.ReadFile("topics/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("no docs topic %q (available: %s)", name, strings.Join(Topics(), ", "))
	}
	return string(data), nil
}
