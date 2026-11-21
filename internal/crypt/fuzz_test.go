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
	f.Add(c.Encrypt([]byte("A=1\n")))
	f.Add(append([]byte{}, Magic...))
	f.Add([]byte("plain"))
	f.Fuzz(func(t *testing.T, data []byte) {
		plain, err := c.Decrypt(data)
		if err != nil {
			return
		}
		if !bytes.Equal(c.Encrypt(plain), data) {
			t.Fatalf("accepted ciphertext that Encrypt would not produce")
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
