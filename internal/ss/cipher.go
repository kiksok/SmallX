package ss

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

type CipherDef struct {
	Name    string
	KeySize int
	newAEAD func([]byte) (cipher.AEAD, error)
}

var cipherDefs = map[string]CipherDef{
	"aes-128-gcm": {
		Name:    "aes-128-gcm",
		KeySize: 16,
		newAEAD: newAESGCM,
	},
	"aes-192-gcm": {
		Name:    "aes-192-gcm",
		KeySize: 24,
		newAEAD: newAESGCM,
	},
	"aes-256-gcm": {
		Name:    "aes-256-gcm",
		KeySize: 32,
		newAEAD: newAESGCM,
	},
	"chacha20-ietf-poly1305": {
		Name:    "chacha20-ietf-poly1305",
		KeySize: chacha20poly1305.KeySize,
		newAEAD: chacha20poly1305.New,
	},
}

func LookupCipher(name string) (CipherDef, bool) {
	def, ok := cipherDefs[strings.ToLower(strings.TrimSpace(name))]
	return def, ok
}

func (c CipherDef) SaltSize() int {
	if c.KeySize > 16 {
		return c.KeySize
	}
	return 16
}

func (c CipherDef) Overhead() int {
	aead, err := c.newAEAD(make([]byte, c.KeySize))
	if err != nil {
		return 16
	}
	return aead.Overhead()
}

func DeriveMasterKey(def CipherDef, password string) []byte {
	var out, prev []byte
	h := md5.New()
	for len(out) < def.KeySize {
		_, _ = h.Write(prev)
		_, _ = h.Write([]byte(password))
		out = h.Sum(out)
		prev = out[len(out)-h.Size():]
		h.Reset()
	}
	return out[:def.KeySize]
}

func DeriveSessionAEAD(def CipherDef, masterKey, salt []byte) (cipher.AEAD, error) {
	if len(masterKey) != def.KeySize {
		return nil, fmt.Errorf("invalid master key size: need %d bytes", def.KeySize)
	}
	subkey := make([]byte, def.KeySize)
	reader := hkdf.New(sha1.New, masterKey, salt, []byte("ss-subkey"))
	if _, err := io.ReadFull(reader, subkey); err != nil {
		return nil, err
	}
	return def.newAEAD(subkey)
}

func newAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

