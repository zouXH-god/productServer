package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

func secretKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("SECRET_ENCRYPTION_KEY is required")
	}
	if b, e := base64.StdEncoding.DecodeString(raw); e == nil && len(b) == 32 {
		return b, nil
	}
	if b, e := hex.DecodeString(raw); e == nil && len(b) == 32 {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("SECRET_ENCRYPTION_KEY must contain 32 bytes")
}
func encryptSecret(master, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	k, e := secretKey(master)
	if e != nil {
		return "", e
	}
	block, e := aes.NewCipher(k)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(value), nil)), nil
}
func decryptSecret(master, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	k, e := secretKey(master)
	if e != nil {
		return "", e
	}
	data, e := base64.StdEncoding.DecodeString(value)
	if e != nil {
		return "", e
	}
	block, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(block)
	if len(data) < g.NonceSize() {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	plain, e := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], nil)
	return string(plain), e
}
