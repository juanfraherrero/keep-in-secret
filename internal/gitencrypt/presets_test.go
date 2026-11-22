package gitencrypt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithPresetsAddsOnlyMissingPatterns(t *testing.T) {
	tf, _ := FindPreset("terraform")
	got := WithPresets(DefaultContent+"*.tfvars\n", []Preset{tf})
	if strings.Count(got, "*.tfvars\n") != 1 {
		t.Errorf("*.tfvars duplicated:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n# terraform: variable files\n*.tfvars.json\n!*.tfvars.example\n") {
		t.Errorf("unexpected tail:\n%s", got)
	}
	if again := WithPresets(got, []Preset{tf}); again != got {
		t.Errorf("not idempotent:\n%s", again)
	}
}

func TestFindPresetUnknown(t *testing.T) {
	_, err := FindPreset("cobol")
	if err == nil || !strings.Contains(err.Error(), "android, rails, spring, terraform") {
		t.Fatalf("err = %v", err)
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"infra/main.tf", "pom.xml"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	var names []string
	for _, p := range Detect(root) {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "spring,terraform" {
		t.Fatalf("detected %v", names)
	}
}

// Every preset pattern must be valid .gitencrypt and survive Block.
func TestPresetPatternsRender(t *testing.T) {
	for _, p := range Presets {
		b := Block(WithPresets("", []Preset{p}))
		for _, pat := range p.Patterns {
			if !strings.Contains(b, strings.TrimPrefix(pat, "!")+" ") {
				t.Errorf("%s: %s missing from block", p.Name, pat)
			}
		}
	}
}
