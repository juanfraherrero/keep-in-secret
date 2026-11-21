package gitx

import "testing"

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
