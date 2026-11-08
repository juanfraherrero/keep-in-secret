package crypt

import (
	"bytes"
	"testing"
)

func TestRoundTripDeterministic(t *testing.T) {
	c, err := New(NewKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range [][]byte{nil, []byte("A=1\n"), bytes.Repeat([]byte("x"), 1<<20)} {
		a, b := c.Encrypt(plain), c.Encrypt(plain)
		if !bytes.Equal(a, b) {
			t.Fatal("encryption is not deterministic")
		}
		if !IsEncrypted(a) {
			t.Fatal("missing header")
		}
		if !bytes.Equal(c.Encrypt(a), a) {
			t.Fatal("encrypting ciphertext must be a no-op")
		}
		got, err := c.Decrypt(a)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("round trip failed: %v", err)
		}
	}
}

func TestRejectsTamperingAndWrongKey(t *testing.T) {
	c1, _ := New(NewKey())
	c2, _ := New(NewKey())
	ct := c1.Encrypt([]byte("SECRET=1"))
	if _, err := c2.Decrypt(ct); err != ErrAuth {
		t.Fatalf("wrong key: got %v", err)
	}
	ct[len(ct)-1] ^= 1
	if _, err := c1.Decrypt(ct); err != ErrAuth {
		t.Fatalf("tampered: got %v", err)
	}
	if _, err := c1.Decrypt([]byte("plain")); err != ErrNotEncrypted {
		t.Fatalf("plain: got %v", err)
	}
}
