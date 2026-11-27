package gitencrypt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCatalogParses checks that every copy-paste block in the patterns
// catalog is valid .gitencrypt, so the docs can't drift into lines kis
// rejects or reads differently than written.
func TestCatalogParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "gitencrypt-patterns.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(DefaultContent, "/blob/main/docs/gitencrypt-patterns.md\n") {
		t.Errorf("DefaultContent does not link to docs/gitencrypt-patterns.md:\n%s", DefaultContent)
	}
	blocks := catalogBlocks(string(data))
	if len(blocks) < 8 {
		t.Fatalf("found %d ```gitignore blocks in the catalog, want at least 8", len(blocks))
	}
	for _, b := range blocks {
		rules, errs := Parse(b)
		for _, err := range errs {
			t.Errorf("%v\nin block:\n%s", err, b)
		}
		if len(rules) == 0 {
			t.Errorf("block has no patterns:\n%s", b)
		}
		for _, r := range rules {
			if r.Pattern == "" || strings.Contains(r.Pattern, "`") {
				t.Errorf("suspicious pattern %q in block:\n%s", r.Pattern, b)
			}
		}
	}
}

// catalogBlocks returns the contents of the ```gitignore fences in markdown.
func catalogBlocks(md string) []string {
	var blocks, cur []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		switch {
		case !in && strings.TrimSpace(line) == "```gitignore":
			in, cur = true, nil
		case in && strings.TrimSpace(line) == "```":
			in = false
			blocks = append(blocks, strings.Join(cur, "\n")+"\n")
		case in:
			cur = append(cur, line)
		}
	}
	return blocks
}
