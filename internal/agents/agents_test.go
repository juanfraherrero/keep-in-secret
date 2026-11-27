package agents

import (
	"os"
	"strings"
	"testing"
)

// The section kis writes and docs/llms.txt are two copies of the agent rules.
// The rules that keep secrets from leaking must say the same in both.
func TestBlockAndLLMsTxtAgree(t *testing.T) {
	raw, err := os.ReadFile("../../docs/llms.txt")
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{
		"Block":         strings.Join(strings.Fields(Block), " "),
		"docs/llms.txt": strings.Join(strings.Fields(string(raw)), " "),
	}
	for name, text := range docs {
		for _, want := range []string{
			"copy secret values into code, logs, commit messages,",
			"If a task seems to need one, refer to the variable by name, or ask the developer.",
			"`kis status --check --since origin/main`",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if strings.Contains(text, "unless the task requires it") {
			t.Errorf("%s still says %q", name, "unless the task requires it")
		}
	}
}
