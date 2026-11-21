package crypt

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// The v1 file format is frozen: these fixtures were produced by kis v1 and
// every future version must decrypt them and, given the same key and
// plaintext, produce identical bytes. Otherwise existing repositories would
// fail to unlock, or every encrypted file would suddenly look modified.
func TestFormatV1Frozen(t *testing.T) {
	keyHex, err := os.ReadFile("testdata/v1/master.hex")
	if err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(keyHex)))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := os.ReadFile("testdata/v1/plain.env")
	want, _ := os.ReadFile("testdata/v1/cipher.bin")

	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decrypt(want)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("cannot decrypt v1 fixture: %v", err)
	}
	if !bytes.Equal(c.Encrypt(plain), want) {
		t.Fatal("encryption output changed: v1 repositories would show every secret as modified")
	}
}
