package gitx

import (
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for in, want := range map[string][3]int{
		"git version 2.39.5":                 {2, 39, 5},
		"git version 2.39.5 (Apple Git-154)": {2, 39, 5},
		"git version 2.45.1.windows.1":       {2, 45, 1},
		"git version 2.28":                   {2, 28, 0},
		"git version 3.0.0-rc1":              {3, 0, 0},
		"something else":                     {}, // rejected
	} {
		got, err := ParseVersion(in)
		if want == ([3]int{}) {
			if err == nil {
				t.Errorf("%q: expected error", in)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %v, %v", in, got, err)
		}
	}
}

func TestSupported(t *testing.T) {
	for v, want := range map[[3]int]bool{
		{2, 28, 0}: true, {2, 27, 9}: false, {2, 55, 0}: true, {3, 0, 0}: true, {1, 99, 0}: false,
	} {
		if Supported(v) != want {
			t.Errorf("%v: got %v", v, !want)
		}
	}
}

func TestVersionError(t *testing.T) {
	if err := VersionError([3]int{2, 28, 0}); err != nil {
		t.Errorf("2.28.0: unexpected error %v", err)
	}
	err := VersionError([3]int{2, 25, 1})
	if err == nil {
		t.Fatal("2.25.1: expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"2.28", "found 2.25.1", "Upgrade git"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	// No command that may not work on this machine (see CLAUDE.md).
	for _, cmd := range []string{"brew", "winget", "apt", "dnf", "apk", "sudo"} {
		if strings.Contains(msg, cmd) {
			t.Errorf("message %q suggests %q", msg, cmd)
		}
	}
}
