package gitencrypt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchesGitignoreSemantics checks, against real git, that a path is
// encrypted exactly when the same patterns in a .gitignore would match it.
//
// One divergence is intentional: gitignore cannot re-include a file inside an
// excluded directory ("dir/" then "!dir/keep"), but .gitencrypt can, so those
// cases list the expected result explicitly.
func TestMatchesGitignoreSemantics(t *testing.T) {
	cases := []struct {
		patterns string
		paths    []string
		override map[string]bool // expected encryption where gitignore differs
	}{
		{".env\n", []string{".env", "a/.env", "a/b/.env", ".env.local", "x.env"}, nil},
		{".env.*\n!.env.example\n", []string{".env.local", "a/.env.prod", ".env.example", "a/.env.example", ".env"}, nil},
		{DefaultContent, []string{".env", ".env.production", "apps/web/.env", ".env.local", ".env.development.local", ".env.production.local", ".env.test.local", "apps/web/.env.local", ".env.local.bak", ".env.localhost", ".env.example", ".env.local.example", "x.local"}, nil},
		{"*.pem\n!public.pem\n", []string{"a.pem", "d/b.pem", "public.pem", "d/public.pem", "pem"}, nil},
		{"/config/prod.yaml\n", []string{"config/prod.yaml", "x/config/prod.yaml", "prod.yaml"}, nil},
		{"config/prod.yaml\n", []string{"config/prod.yaml", "x/config/prod.yaml"}, nil},
		{"secrets/\n", []string{"secrets/a", "secrets/b/c", "x/secrets/a", "secrets"}, nil},
		{"secrets\n", []string{"secrets", "secrets/a", "secrets/b/c", "x/secrets/a"}, nil},
		{"/secrets/\n", []string{"secrets/a", "x/secrets/a"}, nil},
		{"**/keys/*.json\n", []string{"keys/a.json", "a/b/keys/c.json", "keys/sub/c.json"}, nil},
		{"**/config/master.key\n**/config/credentials/*.key\n", []string{"config/master.key", "apps/web/config/master.key", "config/credentials/production.key", "apps/web/config/credentials/staging.key", "apps/web/config/credentials/staging.yml.enc"}, nil},
		{"deploy/**/*.key\n", []string{"deploy/a.key", "deploy/x/y/b.key", "other/a.key"}, nil},
		{"*.secret\n!keep/*.secret\n", []string{"a.secret", "keep/b.secret", "keep/sub/c.secret"}, nil},
		{"\\#hash\n\\!bang\n", []string{"#hash", "!bang"}, nil},
		{"my file.txt\n", []string{"my file.txt", "d/my file.txt"}, nil},
		{"secrets/\n!secrets/README.md\n", []string{"secrets/a", "secrets/README.md"},
			map[string]bool{"secrets/README.md": false}},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(strings.TrimSpace(tc.patterns), "\n", " "), func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-q")
			write(t, filepath.Join(dir, ".gitignore"), tc.patterns)
			write(t, filepath.Join(dir, ".gitattributes"), Block(tc.patterns))
			for _, p := range tc.paths {
				ignored := git(t, dir, "check-ignore", "-q", "--no-index", "--", p) == nil
				want := ignored
				if v, ok := tc.override[p]; ok {
					want = v
				}
				out, err := exec.Command("git", "-C", dir, "check-attr", "filter", "--", p).Output()
				if err != nil {
					t.Fatal(err)
				}
				got := strings.HasSuffix(strings.TrimSpace(string(out)), ": kis")
				if got != want {
					t.Errorf("%q: encrypted=%v, want %v (gitignore matches: %v)\n%s", p, got, want, ignored, Block(tc.patterns))
				}
			}
		})
	}
}

// git runs git and returns its error (nil on exit 0).
func git(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	err := cmd.Run()
	if _, ok := err.(*exec.ExitError); err != nil && !ok {
		t.Fatal(err)
	}
	return err
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
