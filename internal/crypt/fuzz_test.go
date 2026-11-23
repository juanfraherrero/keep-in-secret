package crypt

import (
	"bytes"
	"testing"
)

var fuzzKey = bytes.Repeat([]byte{7}, KeySize)

// Decrypt must never panic, and anything it accepts must be exactly what
// Encrypt produces for the recovered plaintext (no malleability).
func FuzzDecrypt(f *testing.F) {
	c, _ := New(fuzzKey)
	g, _ := NewGroup(fuzzKey, "prod")
	f.Add(c.Encrypt([]byte("A=1\n")))
	f.Add(g.Encrypt([]byte("A=1\n")))
	f.Add(append([]byte{}, Magic...))
	f.Add(append(append([]byte{}, MagicV2...), 200))
	f.Add([]byte("plain"))
	f.Fuzz(func(t *testing.T, data []byte) {
		GroupOf(data) // must not panic either
		for _, c := range []*Cipher{c, g} {
			plain, err := c.Decrypt(data)
			if err != nil {
				continue
			}
			if !bytes.Equal(c.Encrypt(plain), data) {
				t.Fatalf("accepted ciphertext that Encrypt would not produce")
			}
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	c, _ := New(fuzzKey)
	f.Add([]byte(""))
	f.Add([]byte("DB_PASS=hunter2\n"))
	f.Fuzz(func(t *testing.T, plain []byte) {
		ct := c.Encrypt(plain)
		if IsEncrypted(plain) {
			if !bytes.Equal(ct, plain) {
				t.Fatal("ciphertext-looking input must pass through")
			}
			return
		}
		got, err := c.Decrypt(ct)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("round trip failed: %v", err)
		}
	})
}
