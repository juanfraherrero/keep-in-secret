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

func TestGroups(t *testing.T) {
	master := NewKey()
	def, _ := New(master)
	prod, err := NewGroup(master, "prod")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("DB=prod\n")
	ct := prod.Encrypt(plain)
	if !IsEncrypted(ct) || !bytes.HasPrefix(ct, MagicV2) {
		t.Fatalf("group file must use v2: %q", ct[:12])
	}
	if g, ok := GroupOf(ct); !ok || g != "prod" {
		t.Fatalf("GroupOf = %q, %v", g, ok)
	}
	if g, _ := GroupOf(def.Encrypt(plain)); g != DefaultGroup {
		t.Fatalf("default file group = %q", g)
	}
	if got, err := prod.Decrypt(ct); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip: %v", err)
	}
	// Same master key, other group: refused before touching the crypto.
	if _, err := def.Decrypt(ct); err != ErrWrongGroup {
		t.Fatalf("default cipher on prod file: %v", err)
	}
	// Renaming the group in the header breaks authentication.
	staging, _ := NewGroup(master, "stage")
	forged := append(append([]byte{}, MagicV2...), byte(5))
	forged = append(append(forged, "stage"...), ct[len(MagicV2)+1+4:]...)
	if _, err := staging.Decrypt(forged); err != ErrAuth {
		t.Fatalf("forged group: %v", err)
	}
	if _, err := NewGroup(master, ""); err == nil {
		t.Fatal("empty group accepted")
	}
}
