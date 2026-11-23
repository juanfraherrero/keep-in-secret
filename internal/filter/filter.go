// Package filter implements the git clean/smudge filter, both as one-shot
// commands and as git's long-running filter process protocol
// (gitattributes(5), "Long Running Filter Process"), which lets git start kis
// once per command instead of once per file.
package filter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
)

// Keys gives the filter the cipher of each group and the group each path
// belongs to (its kis-group attribute; crypt.DefaultGroup when unset).
type Keys struct {
	Cipher  func(group string) (*crypt.Cipher, error) // error when that key is unavailable
	GroupOf func(path string) (string, error)
}

// Clean encrypts plaintext on its way into git with the key of the path's
// group. It refuses to store plaintext when that key is unavailable, since
// that would leak the secret. Ciphertext passes through, so a file whose
// group you are not in can still be committed alongside your changes.
func Clean(data []byte, path string, keys Keys) ([]byte, error) {
	if crypt.IsEncrypted(data) {
		return data, nil
	}
	group, err := keys.GroupOf(path)
	if err != nil {
		return nil, fmt.Errorf("refusing to store %s in plaintext: %w", path, err)
	}
	c, err := keys.Cipher(group)
	if err != nil {
		return nil, fmt.Errorf("refusing to store %s in plaintext: %w", path, err)
	}
	return c.Encrypt(data), nil
}

// Smudge decrypts content on its way out of git, with the key of the group
// named in its header. Without that key, or with one that does not match,
// the ciphertext is passed through unchanged so checkouts never fail.
func Smudge(data []byte, path string, keys Keys) []byte {
	group, ok := crypt.GroupOf(data)
	if !ok {
		return data
	}
	c, err := keys.Cipher(group)
	if err != nil {
		return data
	}
	plain, err := c.Decrypt(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kis: cannot decrypt %s: %v\n", path, err)
		return data
	}
	return plain
}

// Cached wraps keys so each group's key is loaded at most once.
func Cached(keys Keys) Keys {
	type entry struct {
		c   *crypt.Cipher
		err error
	}
	loaded := map[string]entry{}
	cipher := func(group string) (*crypt.Cipher, error) {
		e, ok := loaded[group]
		if !ok {
			e.c, e.err = keys.Cipher(group)
			loaded[group] = e
		}
		return e.c, e.err
	}
	return Keys{Cipher: cipher, GroupOf: keys.GroupOf}
}

const maxPktData = 65516

type pktReader struct{ r *bufio.Reader }

// read returns one packet; flush packets return (nil, true, nil).
func (p pktReader) read() ([]byte, bool, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(p.r, hdr[:]); err != nil {
		return nil, false, err
	}
	var n int
	if _, err := fmt.Sscanf(string(hdr[:]), "%04x", &n); err != nil {
		return nil, false, fmt.Errorf("bad pkt-line header %q", hdr)
	}
	if n == 0 {
		return nil, true, nil
	}
	if n < 4 {
		return nil, false, fmt.Errorf("bad pkt-line length %d", n)
	}
	buf := make([]byte, n-4)
	_, err := io.ReadFull(p.r, buf)
	return buf, false, err
}

// readText reads text packets until a flush, returning key=value pairs.
func (p pktReader) readText() (map[string]string, error) {
	m := map[string]string{}
	for {
		b, flush, err := p.read()
		if err != nil {
			return nil, err
		}
		if flush {
			return m, nil
		}
		k, v, _ := strings.Cut(strings.TrimSuffix(string(b), "\n"), "=")
		m[k] = v
	}
}

func (p pktReader) readContent() ([]byte, error) {
	var out []byte
	for {
		b, flush, err := p.read()
		if err != nil {
			return nil, err
		}
		if flush {
			return out, nil
		}
		out = append(out, b...)
	}
}

type pktWriter struct{ w *bufio.Writer }

func (p pktWriter) data(b []byte) error {
	if _, err := fmt.Fprintf(p.w, "%04x", len(b)+4); err != nil {
		return err
	}
	_, err := p.w.Write(b)
	return err
}

func (p pktWriter) text(s string) error { return p.data([]byte(s + "\n")) }

func (p pktWriter) flush() error {
	if _, err := p.w.WriteString("0000"); err != nil {
		return err
	}
	return p.w.Flush()
}

func (p pktWriter) content(b []byte) error {
	for len(b) > 0 {
		n := min(len(b), maxPktData)
		if err := p.data(b[:n]); err != nil {
			return err
		}
		b = b[n:]
	}
	return p.flush()
}

// Process speaks git's long-running filter protocol on r/w until git closes
// the pipe.
func Process(r io.Reader, w io.Writer, keys Keys) error {
	in := pktReader{bufio.NewReader(r)}
	out := pktWriter{bufio.NewWriter(w)}

	hello, err := in.readText()
	if err != nil {
		return err
	}
	if _, ok := hello["git-filter-client"]; !ok || hello["version"] != "2" {
		return errors.New("unsupported filter protocol handshake")
	}
	if err := out.text("git-filter-server"); err != nil {
		return err
	}
	if err := out.text("version=2"); err != nil {
		return err
	}
	if err := out.flush(); err != nil {
		return err
	}
	if _, err := in.readText(); err != nil { // git's capability list
		return err
	}
	for _, c := range []string{"capability=clean", "capability=smudge"} {
		if err := out.text(c); err != nil {
			return err
		}
	}
	if err := out.flush(); err != nil {
		return err
	}

	for {
		req, err := in.readText()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		data, err := in.readContent()
		if err != nil {
			return err
		}
		path := req["pathname"]
		var result []byte
		switch req["command"] {
		case "clean":
			result, err = Clean(data, path, keys)
		case "smudge":
			result = Smudge(data, path, keys)
		default:
			err = fmt.Errorf("unknown command %q", req["command"])
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "kis: %v\n", err)
			if err := out.text("status=error"); err != nil {
				return err
			}
			if err := out.flush(); err != nil {
				return err
			}
			continue
		}
		if err := out.text("status=success"); err != nil {
			return err
		}
		if err := out.flush(); err != nil {
			return err
		}
		if err := out.content(result); err != nil {
			return err
		}
		if err := out.flush(); err != nil { // empty list: keep status=success
			return err
		}
	}
}
