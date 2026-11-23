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
// File format v1, used for the default group:
//
//	Magic (8 bytes) | nonce (12 bytes) | GCM ciphertext+tag
//
// Format v2, for files in a named group (each group has its own master key):
//
//	MagicV2 (8 bytes) | group length (1 byte) | group | nonce | ciphertext+tag
//
// The v2 header is the GCM additional data, so a group name cannot be
// swapped without failing authentication. Default-group files stay v1 so
// repositories that never use groups see byte-identical ciphertext.
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

// MagicV2 prefixes files encrypted for a named group.
var MagicV2 = []byte("\x00KIS\x00v2\x00")

// DefaultGroup holds every file .gitencrypt doesn't assign to a group.
const DefaultGroup = "default"

var ErrNotEncrypted = errors.New("content is not kis-encrypted")
var ErrAuth = errors.New("decryption failed: wrong key or corrupted content")
var ErrWrongGroup = errors.New("encrypted for a different group")

// NewKey returns a fresh random master key.
func NewKey() []byte {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}

// IsEncrypted reports whether data carries a kis header.
func IsEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, Magic) || bytes.HasPrefix(data, MagicV2)
}

// GroupOf returns the group data was encrypted for, read from its header.
func GroupOf(data []byte) (string, bool) {
	if bytes.HasPrefix(data, Magic) {
		return DefaultGroup, true
	}
	if h, ok := headerV2(data); ok {
		return string(h[len(MagicV2)+1:]), true
	}
	return "", false
}

// headerV2 returns the v2 header (magic, length byte and group name).
func headerV2(data []byte) ([]byte, bool) {
	if !bytes.HasPrefix(data, MagicV2) || len(data) <= len(MagicV2) {
		return nil, false
	}
	end := len(MagicV2) + 1 + int(data[len(MagicV2)])
	if end > len(data) || end == len(MagicV2)+1 {
		return nil, false
	}
	return data[:end], true
}

// Cipher encrypts and decrypts file contents with keys derived from a master key.
type Cipher struct {
	aead   cipher.AEAD
	macKey []byte
	header []byte // what precedes the nonce: Magic, or the v2 group header
}

// New returns the cipher for the default group.
func New(master []byte) (*Cipher, error) { return NewGroup(master, DefaultGroup) }

// NewGroup returns the cipher for files in group, keyed by that group's master key.
func NewGroup(master []byte, group string) (*Cipher, error) {
	if len(master) != KeySize {
		return nil, errors.New("invalid master key size")
	}
	header := Magic
	if group != DefaultGroup {
		if group == "" || len(group) > 255 {
			return nil, errors.New("invalid group name")
		}
		header = append(append(append([]byte{}, MagicV2...), byte(len(group))), group...)
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
	return &Cipher{aead: aead, macKey: macKey, header: header}, nil
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
	out := make([]byte, 0, len(c.header)+nonceSize+len(plain)+c.aead.Overhead())
	out = append(out, c.header...)
	out = append(out, nonce...)
	return c.aead.Seal(out, nonce, plain, c.header)
}

func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, ErrNotEncrypted
	}
	if !bytes.HasPrefix(data, c.header) {
		return nil, ErrWrongGroup
	}
	rest := data[len(c.header):]
	if len(rest) < nonceSize+c.aead.Overhead() {
		return nil, ErrAuth
	}
	nonce, ct := rest[:nonceSize], rest[nonceSize:]
	plain, err := c.aead.Open(nil, nonce, ct, c.header)
	if err != nil {
		return nil, ErrAuth
	}
	if !hmac.Equal(nonce, c.nonce(plain)) {
		return nil, ErrAuth
	}
	return plain, nil
}
