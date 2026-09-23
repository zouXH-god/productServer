package server

import (
	"strconv"

	securitypkg "productserver/internal/security"
)

func itoa[T ~uint](value T) string { return strconv.FormatUint(uint64(value), 10) }

func secretKey(raw string) ([]byte, error) { return securitypkg.SecretKey(raw) }

func encryptSecret(master, value string) (string, error) {
	return securitypkg.EncryptSecret(master, value)
}

func decryptSecret(master, value string) (string, error) {
	return securitypkg.DecryptSecret(master, value)
}
