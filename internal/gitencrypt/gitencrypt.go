// Package gitencrypt translates .gitencrypt (gitignore-style patterns) into a
// managed block of .gitattributes lines that route matching files through the
// kis filter.
package gitencrypt

import (
	"strconv"
	"strings"
)

const (
	FileName  = ".gitencrypt"
	beginMark = "# >>> kis: generated from .gitencrypt, do not edit (run `kis sync`)"
	endMark   = "# <<< kis"
)

const DefaultContent = `# Files matching these patterns are encrypted in git by kis (keep-in-secret).
# Same syntax as .gitignore. kis keeps .gitattributes in sync; run "kis sync"
# after editing (the pre-commit check will remind you).
.env
.env.*
!.env.example
!.env.sample
!.env.template
`

// alwaysPlain lists paths kis needs to read without decrypting. They come last
// in the block so a broad pattern like "*" cannot encrypt them.
var alwaysPlain = []string{".gitencrypt", ".gitattributes", ".gitignore", ".kis/**"}

// Block renders the managed .gitattributes block for the given .gitencrypt content.
func Block(gitencrypt string) string {
	var b strings.Builder
	b.WriteString(beginMark + "\n")
	for _, line := range strings.Split(gitencrypt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := strings.HasPrefix(line, "!")
		if negate {
			line = line[1:]
		}
		line = strings.TrimPrefix(line, `\`) // gitignore escape for leading # or !
		if strings.HasSuffix(line, "/") {
			// gitattributes patterns never match directories themselves.
			line += "**"
		}
		if negate {
			b.WriteString(quote(line) + " !filter !diff\n")
		} else {
			b.WriteString(quote(line) + " filter=kis diff=kis\n")
		}
	}
	for _, p := range alwaysPlain {
		b.WriteString(p + " !filter !diff\n")
	}
	b.WriteString(endMark + "\n")
	return b.String()
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
