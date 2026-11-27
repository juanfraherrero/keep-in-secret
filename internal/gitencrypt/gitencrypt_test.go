package gitencrypt

import (
	"strings"
	"testing"
)

func TestBlock(t *testing.T) {
	got := Block("# comment\n.env\n!.env.example\nsecrets/\nmy file.txt\n\n")
	for _, want := range []string{
		".env filter=kis diff=kis merge=kis\n",
		".env.example !filter !diff !merge\n",
		"**/secrets/** filter=kis diff=kis merge=kis\n",
		"**/.env/** filter=kis diff=kis merge=kis\n",
		`"my file.txt" filter=kis diff=kis merge=kis` + "\n",
		".kis/** !filter !diff !merge\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "comment") {
		t.Error("comments must be dropped")
	}
}

// TestIsLocal pins which names the local-file guard covers (#13): the names
// DefaultContent excludes, at any depth, whatever their case.
func TestIsLocal(t *testing.T) {
	if !strings.Contains(DefaultContent, "\n!"+LocalPattern+"\n") {
		t.Errorf("DefaultContent does not exclude %s:\n%s", LocalPattern, DefaultContent)
	}
	for p, want := range map[string]bool{
		".env.local": true, ".env.development.local": true, ".env.test.local": true,
		"apps/web/.env.local": true, ".env.LOCAL": true, "a/.Env.Production.Local": true,
		".env": false, ".env.production": false, ".env.local.bak": false, ".env.localhost": false,
		".env.local.example": false, "x.local": false, ".env.local/secret": false, "README.md": false,
	} {
		if got := IsLocal(p); got != want {
			t.Errorf("IsLocal(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestMergeReplacesBlock(t *testing.T) {
	attrs := "*.png binary\n"
	once := Merge(attrs, Block(".env\n"))
	twice := Merge(once, Block(".env\n.secret\n"))
	if !strings.HasPrefix(twice, attrs) {
		t.Fatalf("user lines lost:\n%s", twice)
	}
	if strings.Count(twice, beginMark) != 1 || !strings.Contains(twice, ".secret filter=kis") {
		t.Fatalf("block not replaced:\n%s", twice)
	}
	if !InSync(twice, ".env\n.secret\n") || InSync(twice, ".env\n") {
		t.Fatal("InSync wrong")
	}
}

func TestGroups(t *testing.T) {
	ge := ".env\n.env.*\n.env.production  @prod\nsecrets/ops/ @ops\n!.env.example\n"
	if g := Groups(ge); strings.Join(g, ",") != "ops,prod" {
		t.Fatalf("Groups = %v", g)
	}
	got := Block(ge)
	for _, want := range []string{
		".env filter=kis diff=kis merge=kis !kis-group\n",
		".env.production filter=kis diff=kis merge=kis kis-group=prod\n",
		"secrets/ops/** filter=kis diff=kis merge=kis kis-group=ops\n",
		".env.example !filter !diff !merge\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	// Without groups the block is exactly what older kis versions wrote.
	if strings.Contains(Block(".env\n"), "kis-group") {
		t.Error("ungrouped block mentions kis-group")
	}
	if _, errs := Parse(ge); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

// Patterns with spaces or an "@" that isn't a group are still fine.
func TestParseNoFalseGroup(t *testing.T) {
	for _, in := range []string{"my file.env\n", "user@host.env\n", "a b@c.env\n", ".env.prod @prod\n"} {
		if _, errs := Parse(in); len(errs) > 0 {
			t.Errorf("Parse(%q) errs = %v, want none", in, errs)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for in, want := range map[string]string{
		".env.prod @prod @ops\n":  "one group",
		"!.env.example @prod\n":   "takes no group",
		".env.prod @pr/od\n":      "invalid group",
		".env.prod @bad group\n":  "looks like a group",
		".env.prod @bad\tgroup\n": "looks like a group",
	} {
		_, errs := Parse(in)
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), want) {
			t.Errorf("Parse(%q) errs = %v, want %q", in, errs, want)
		}
		_ = Block(in) // still renders
	}
}
