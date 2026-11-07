// Package crypt implements the deterministic, authenticated encryption used
// for files stored in git.
//
// Encryption must be deterministic: git compares blobs by content, so the same
// plaintext has to produce the same ciphertext or every encrypted file would
// always look modified. We use AES-256-GCM with a synthetic nonce derived from
// HMAC-SHA256(macKey, plaintext). A nonce only repeats when the plaintext is
// identical, in which case the ciphertext is identical too, so GCM's
// nonce-reuse problem never arises. Decryption re-derives the nonce from the
// recovered plaintext and rejects any mismatch.
//
// File format: Magic (8 bytes) | nonce (12 bytes) | GCM ciphertext+tag.
package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// KeySize is the size in bytes of a repository master key.
const KeySize = 32

const nonceSize = 12

// Magic prefixes every encrypted file. It starts with a NUL byte so git treats
// encrypted content as binary.
var Magic = []byte("\x00KIS\x00v1\x00")

var ErrNotEncrypted = errors.New("content is not kis-encrypted")
var ErrAuth = errors.New("decryption failed: wrong key or corrupted content")

// NewKey returns a fresh random master key.
func NewKey() []byte {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}

// IsEncrypted reports whether data carries the kis header.
func IsEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, Magic)
}

// Cipher encrypts and decrypts file contents with keys derived from a master key.
type Cipher struct {
	aead   cipher.AEAD
	macKey []byte
}

func New(master []byte) (*Cipher, error) {
	if len(master) != KeySize {
		return nil, errors.New("invalid master key size")
	}
	encKey, err := hkdf.Key(sha256.New, master, nil, "kis file encryption v1", 32)
	if err != nil {
		return nil, err
	}
	macKey, err := hkdf.Key(sha256.New, master, nil, "kis file nonce v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, macKey: macKey}, nil
}

func (c *Cipher) nonce(plain []byte) []byte {
	m := hmac.New(sha256.New, c.macKey)
	m.Write(plain)
	return m.Sum(nil)[:nonceSize]
}

// Encrypt returns the encrypted form of plain. Content that is already
// encrypted is returned unchanged so the clean filter is idempotent.
func (c *Cipher) Encrypt(plain []byte) []byte {
	if IsEncrypted(plain) {
		return plain
	}
	nonce := c.nonce(plain)
	out := make([]byte, 0, len(Magic)+nonceSize+len(plain)+c.aead.Overhead())
	out = append(out, Magic...)
	out = append(out, nonce...)
	return c.aead.Seal(out, nonce, plain, Magic)
}

func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, ErrNotEncrypted
	}
	rest := data[len(Magic):]
	if len(rest) < nonceSize+c.aead.Overhead() {
		return nil, ErrAuth
	}
	nonce, ct := rest[:nonceSize], rest[nonceSize:]
	plain, err := c.aead.Open(nil, nonce, ct, Magic)
	if err != nil {
		return nil, ErrAuth
	}
	if !hmac.Equal(nonce, c.nonce(plain)) {
		return nil, ErrAuth
	}
	return plain, nil
}
