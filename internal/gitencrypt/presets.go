package gitencrypt

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Preset is a set of .gitencrypt patterns for one stack's secret files.
// .env is always covered by DefaultContent; presets add what other
// ecosystems use instead.
type Preset struct {
	Name     string
	Summary  string
	Patterns []string
	// Markers are globs, relative to the repository root, whose presence
	// suggests the stack. Detection only suggests; it never adds patterns.
	Markers []string
}

var Presets = []Preset{
	{
		Name: "android", Summary: "signing keystores and their passwords",
		Patterns: []string{"*.jks", "*.keystore", "keystore.properties"},
		Markers:  []string{"app/src/main/AndroidManifest.xml", "*/src/main/AndroidManifest.xml"},
	},
	{
		Name: "rails", Summary: "the key that opens Rails encrypted credentials",
		Patterns: []string{"config/master.key", "config/credentials/*.key"},
		Markers:  []string{"config/application.rb"},
	},
	{
		Name: "spring", Summary: "local Spring profiles",
		Patterns: []string{"application-local.properties", "application-local.yml", "application-local.yaml"},
		Markers:  []string{"pom.xml", "build.gradle", "build.gradle.kts"},
	},
	{
		Name: "terraform", Summary: "variable files",
		Patterns: []string{"*.tfvars", "*.tfvars.json", "!*.tfvars.example"},
		Markers:  []string{"*.tf", "*/*.tf"},
	},
}

// FindPreset looks a preset up by name.
func FindPreset(name string) (Preset, error) {
	var names []string
	for _, p := range Presets {
		if p.Name == name {
			return p, nil
		}
		names = append(names, p.Name)
	}
	return Preset{}, fmt.Errorf("unknown preset %q (available: %s)", name, strings.Join(names, ", "))
}

// Detect returns the presets whose markers exist under root.
func Detect(root string) []Preset {
	var found []Preset
	for _, p := range Presets {
		for _, m := range p.Markers {
			if hits, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(m))); len(hits) > 0 {
				found = append(found, p)
				break
			}
		}
	}
	return found
}

// WithPresets appends each preset's patterns missing from content, under a
// comment naming the preset.
func WithPresets(content string, presets []Preset) string {
	have := map[string]bool{}
	for _, l := range strings.Split(content, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	sort.Slice(presets, func(i, j int) bool { return presets[i].Name < presets[j].Name })
	for _, p := range presets {
		var missing []string
		for _, pat := range p.Patterns {
			if !have[pat] {
				missing = append(missing, pat)
				have[pat] = true
			}
		}
		if len(missing) == 0 {
			continue
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n# " + p.Name + ": " + p.Summary + "\n" + strings.Join(missing, "\n") + "\n"
	}
	return content
}
