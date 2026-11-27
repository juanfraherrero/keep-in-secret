package gitencrypt

import (
	"os"
	"os/exec"
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

// TestCatalogMatches checks, against real git, that the catalog's lines
// encrypt the files each section promises, including in nested folders and
// monorepos. TestCatalogParses only proves the lines are valid; this proves
// they do what the docs say (a Rails line without "**/" parses fine but
// misses apps/web/config/master.key).
func TestCatalogMatches(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "gitencrypt-patterns.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := catalogBlocks(string(data))
	for _, tc := range []struct {
		stack     string
		line      string   // a line that identifies the section's block
		encrypted []string // must be encrypted
		plain     []string // must stay readable
	}{
		{".env", ".env.*\n", []string{".env", ".env.production", "apps/api/.env", "apps/api/.env.production"}, []string{".env.example", "apps/api/.env.sample", "README.md"}},
		{"Android", "*.jks\n", []string{"release.jks", "app/upload.keystore", "app/keystore.properties"}, []string{"app/build.gradle"}},
		{"Rails", "**/config/master.key\n", []string{"config/master.key", "apps/web/config/master.key", "config/credentials/production.key", "apps/web/config/credentials/staging.key"}, []string{"config/credentials/production.yml.enc", "config/application.rb"}},
		{"Spring (local)", "application-local.yml\n", []string{"src/main/resources/application-local.yml"}, []string{"src/main/resources/application.yml"}},
		{"Terraform", "*.tfvars\n", []string{"prod.tfvars", "infra/envs/prod.tfvars", "secrets.auto.tfvars", "prod.tfvars.json"}, []string{"prod.tfvars.example", "main.tf"}},
	} {
		t.Run(tc.stack, func(t *testing.T) {
			var block string
			for _, b := range blocks {
				if strings.Contains("\n"+b, "\n"+tc.line) {
					block = b
					break
				}
			}
			if block == "" {
				t.Fatalf("no catalog block contains the line %q", strings.TrimSpace(tc.line))
			}
			dir := t.TempDir()
			git(t, dir, "init", "-q")
			write(t, filepath.Join(dir, ".gitattributes"), Block(block))
			check := func(paths []string, want bool) {
				for _, p := range paths {
					out, err := exec.Command("git", "-C", dir, "check-attr", "filter", "--", p).Output()
					if err != nil {
						t.Fatal(err)
					}
					if got := strings.HasSuffix(strings.TrimSpace(string(out)), ": kis"); got != want {
						t.Errorf("%s: encrypted=%v, want %v with the catalog's lines:\n%s", p, got, want, block)
					}
				}
			}
			check(tc.encrypted, true)
			check(tc.plain, false)
		})
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
