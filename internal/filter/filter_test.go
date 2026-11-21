package filter

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/juanfraherrero/keep-in-secret/internal/crypt"
)

func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

const flush = "0000"

func request(cmd, path string, content []byte) string {
	var b strings.Builder
	b.WriteString(pkt("command="+cmd+"\n") + pkt("pathname="+path+"\n") + flush)
	for len(content) > 0 {
		n := min(len(content), maxPktData)
		b.WriteString(pkt(string(content[:n])))
		content = content[n:]
	}
	b.WriteString(flush)
	return b.String()
}

const handshake = "0016git-filter-client\n" + "000eversion=2\n" + flush +
	"0015capability=clean\n" + "0016capability=smudge\n" + "0015capability=delay\n" + flush

func key(t *testing.T) (KeySource, *crypt.Cipher) {
	c, err := crypt.New(bytes.Repeat([]byte{1}, crypt.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return func() (*crypt.Cipher, error) { return c, nil }, c
}

// response parses kis's replies after the handshake into (status, content) pairs.
func responses(t *testing.T, out []byte) []string {
	r := pktReader{bufio.NewReader(bytes.NewReader(out))}
	if _, err := r.readText(); err != nil { // server hello
		t.Fatal(err)
	}
	caps, err := r.readText()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := caps["capability"]; !ok {
		t.Fatalf("no capabilities: %v", caps)
	}
	var res []string
	for {
		st, err := r.readText()
		if err != nil {
			return res
		}
		if st["status"] != "success" {
			res = append(res, st["status"])
			continue
		}
		content, err := r.readContent()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.readText(); err != nil { // trailing status list
			t.Fatal(err)
		}
		res = append(res, string(content))
	}
}

func TestProcessProtocol(t *testing.T) {
	ks, c := key(t)
	big := bytes.Repeat([]byte("x"), 3*maxPktData+17) // spans several packets
	in := handshake +
		request("clean", ".env", []byte("A=1\n")) +
		request("smudge", ".env", c.Encrypt([]byte("A=1\n"))) +
		request("clean", "big", big) +
		request("frobnicate", "x", []byte("?"))
	var out bytes.Buffer
	if err := Process(strings.NewReader(in), &out, ks); err != nil {
		t.Fatal(err)
	}
	res := responses(t, out.Bytes())
	if len(res) != 4 {
		t.Fatalf("got %d responses: %q", len(res), res)
	}
	if got, _ := c.Decrypt([]byte(res[0])); string(got) != "A=1\n" {
		t.Errorf("clean: got %q", res[0])
	}
	if res[1] != "A=1\n" {
		t.Errorf("smudge: got %q", res[1])
	}
	if got, _ := c.Decrypt([]byte(res[2])); !bytes.Equal(got, big) {
		t.Error("multi-packet clean failed")
	}
	if res[3] != "error" {
		t.Errorf("unknown command: got %q", res[3])
	}
}

func TestProcessLockedRefusesClean(t *testing.T) {
	locked := func() (*crypt.Cipher, error) { return nil, errors.New("locked") }
	_, c := key(t)
	ct := c.Encrypt([]byte("S=1"))
	in := handshake + request("clean", ".env", []byte("S=1")) + request("smudge", ".env", ct) + request("clean", ".env", ct)
	var out bytes.Buffer
	if err := Process(strings.NewReader(in), &out, locked); err != nil {
		t.Fatal(err)
	}
	res := responses(t, out.Bytes())
	if res[0] != "error" {
		t.Errorf("plaintext clean while locked must fail, got %q", res[0])
	}
	if res[1] != string(ct) || res[2] != string(ct) {
		t.Error("ciphertext must pass through unchanged while locked")
	}
}

func TestProcessRejectsBadHandshake(t *testing.T) {
	ks, _ := key(t)
	if err := Process(strings.NewReader(pkt("hello\n")+flush), &bytes.Buffer{}, ks); err == nil {
		t.Fatal("expected handshake error")
	}
}

// Malformed input from git must produce an error, never a panic or hang.
func FuzzProcess(f *testing.F) {
	f.Add([]byte(handshake + request("clean", ".env", []byte("A=1"))))
	f.Add([]byte(handshake + "00"))
	f.Add([]byte("0003"))
	f.Fuzz(func(t *testing.T, in []byte) {
		ks, _ := key(t)
		_ = Process(bytes.NewReader(in), &bytes.Buffer{}, ks)
	})
}
