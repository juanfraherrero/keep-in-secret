// Package gitencrypt translates .gitencrypt (gitignore-style patterns) into a
// managed block of .gitattributes lines that route matching files through the
// kis filter.
package gitencrypt

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
)

const (
	FileName  = ".gitencrypt"
	beginMark = "# >>> kis: generated from .gitencrypt, do not edit (run `kis sync`)"
	endMark   = "# <<< kis"
)

const DefaultContent = `# Files matching these patterns are encrypted in git by kis (keep-in-secret).
# Same syntax as .gitignore. kis keeps .gitattributes in sync; run "kis sync"
# after editing (the pre-commit check will remind you).
# Other stacks: https://github.com/juanfraherrero/keep-in-secret/blob/main/docs/gitencrypt-patterns.md
.env
.env.*
!.env.example
!.env.sample
!.env.template
# Local files (.env.local, .env.development.local, ...) hold one developer's
# values: kis never shares them. Delete this line to share them encrypted.
!.env*.local
`

// LocalPattern names local files: one developer's values (.env.local,
// .env.development.local, apps/web/.env.local), gitignored by frameworks such
// as Next.js and Vite. DefaultContent excludes them, and kis refuses to let
// them be committed in plaintext whatever .gitencrypt says.
const LocalPattern = ".env*.local"

// IsLocal reports whether path names a local file, at any depth. It ignores
// case, so .env.LOCAL counts on every OS: .gitencrypt matching follows
// core.ignorecase, and a name it excluded must never slip past the guard.
func IsLocal(path string) bool {
	base := strings.ToLower(path[strings.LastIndex(path, "/")+1:])
	ok, _ := pathpkg.Match(LocalPattern, base)
	return ok
}

// alwaysPlain lists paths kis needs to read without decrypting. They come last
// in the block so a broad pattern like "*" cannot encrypt them.
var alwaysPlain = []string{".gitencrypt", ".gitattributes", ".gitignore", ".kis/**"}

// Rule is one pattern line of .gitencrypt.
type Rule struct {
	Pattern string
	Negate  bool
	Group   string // crypt.DefaultGroup ("default") unless the line ends in @NAME
}

// GroupAttr is the gitattribute naming a file's key group.
const GroupAttr = "kis-group"

const defaultGroup = crypt.DefaultGroup

var validGroup = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Parse reads .gitencrypt. A line may end in " @GROUP" to encrypt its files
// with that group's key instead of the default one. Malformed lines are
// reported in errs and parsed as best as possible, so Block stays total.
// looksLikeGroup reports whether a pattern still contains a word starting
// with "@" after whitespace, like ".env.prod @bad group".
func looksLikeGroup(pattern string) bool {
	for _, f := range strings.Fields(pattern)[1:] {
		if strings.HasPrefix(f, "@") {
			return true
		}
	}
	return false
}

func Parse(content string) (rules []Rule, errs []error) {
	for n, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := Rule{Group: defaultGroup}
		if i := strings.LastIndexAny(line, " \t"); i >= 0 && strings.HasPrefix(line[i+1:], "@") {
			r.Group = line[i+2:]
			line = strings.TrimSpace(line[:i])
			if !validGroup.MatchString(r.Group) {
				errs = append(errs, fmt.Errorf("%s:%d: invalid group %q: use letters, digits, '.', '_' or '-'", FileName, n+1, "@"+r.Group))
				r.Group = defaultGroup
			}
			if j := strings.LastIndexAny(line, " \t"); j >= 0 && strings.HasPrefix(line[j+1:], "@") {
				errs = append(errs, fmt.Errorf("%s:%d: a file belongs to one group; add people to that group instead of listing several", FileName, n+1))
				line = strings.TrimSpace(line[:j])
			}
		}
		// " @" left in the pattern is a group written wrongly (a name with a
		// space, "@bad group"), not a file name: without this the whole line
		// becomes a pattern that matches nothing, and the file stays plaintext.
		if looksLikeGroup(line) {
			errs = append(errs, fmt.Errorf("%s:%d: %q looks like a group, but group names are one word (letters, digits, '.', '_' or '-'): write PATTERN @GROUP", FileName, n+1, line))
		}
		r.Negate = strings.HasPrefix(line, "!")
		r.Pattern = strings.TrimPrefix(line, "!")
		if r.Negate && r.Group != defaultGroup {
			errs = append(errs, fmt.Errorf("%s:%d: a ! line excludes files, it takes no group", FileName, n+1))
			r.Group = defaultGroup
		}
		rules = append(rules, r)
	}
	return rules, errs
}

// Groups lists the named groups .gitencrypt uses, sorted, without "default".
func Groups(content string) []string {
	rules, _ := Parse(content)
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		if r.Group != defaultGroup && !seen[r.Group] {
			seen[r.Group] = true
			out = append(out, r.Group)
		}
	}
	sort.Strings(out)
	return out
}

// Block renders the managed .gitattributes block for the given .gitencrypt content.
func Block(gitencrypt string) string {
	rules, _ := Parse(gitencrypt)
	// Only once groups are in use do default lines need to reset the group
	// (later lines win), so repositories without groups keep the old block.
	grouped := len(Groups(gitencrypt)) > 0
	var b strings.Builder
	b.WriteString(beginMark + "\n")
	for _, r := range rules {
		attrs := " filter=kis diff=kis merge=kis"
		switch {
		case r.Negate:
			attrs = " !filter !diff !merge"
		case r.Group != defaultGroup:
			attrs += " " + GroupAttr + "=" + r.Group
		case grouped:
			attrs += " !" + GroupAttr
		}
		for _, p := range attrPatterns(r.Pattern) {
			b.WriteString(quote(p) + attrs + "\n")
		}
	}
	for _, p := range alwaysPlain {
		b.WriteString(p + " !filter !diff !merge\n")
	}
	// Partners added concurrently on two branches merge without conflict.
	b.WriteString(".kis/recipients.toml merge=union\n")
	b.WriteString(endMark + "\n")
	return b.String()
}

// attrPatterns translates one gitignore pattern into gitattributes patterns
// with the same reach. gitattributes only ever matches files, so a pattern
// that could name a directory also gets a "/**" form for its contents, and a
// pattern without a slash keeps matching at any depth via "**/". Backslash
// escapes (\#, \!) are valid in both syntaxes and pass through unchanged.
func attrPatterns(p string) []string {
	dirOnly := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	contents := "**/" + p + "/**"
	if strings.Contains(p, "/") { // anchored to the .gitencrypt directory
		contents = p + "/**"
	}
	if dirOnly {
		return []string{contents}
	}
	return []string{p, contents}
}

func quote(p string) string {
	if strings.ContainsAny(p, " \t\"") {
		return strconv.Quote(p)
	}
	return p
}

// Merge replaces the managed block inside an existing .gitattributes, or
// appends it when absent.
func Merge(attributes, block string) string {
	start := strings.Index(attributes, beginMark)
	if start >= 0 {
		if end := strings.Index(attributes[start:], endMark); end >= 0 {
			end += start + len(endMark)
			if end < len(attributes) && attributes[end] == '\n' {
				end++
			}
			return attributes[:start] + block + attributes[end:]
		}
	}
	if attributes != "" && !strings.HasSuffix(attributes, "\n") {
		attributes += "\n"
	}
	if attributes != "" {
		attributes += "\n"
	}
	return attributes + block
}

// InSync reports whether attributes already contain the block for gitencrypt.
func InSync(attributes, gitencrypt string) bool {
	return strings.Contains(attributes, Block(gitencrypt))
}
