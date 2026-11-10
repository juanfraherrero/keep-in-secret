package gitencrypt

import (
	"strings"
	"testing"
)

func TestBlock(t *testing.T) {
	got := Block("# comment\n.env\n!.env.example\nsecrets/\nmy file.txt\n\n")
	for _, want := range []string{
		".env filter=kis diff=kis\n",
		".env.example !filter !diff\n",
		"secrets/** filter=kis diff=kis\n",
		`"my file.txt" filter=kis diff=kis` + "\n",
		".kis/** !filter !diff\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "comment") {
		t.Error("comments must be dropped")
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
