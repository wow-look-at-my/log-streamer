package token

import (
	"crypto/rand"
	"encoding/hex"
)

func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Validate(t string) bool {
	if len(t) != 64 {
		return false
	}
	_, err := hex.DecodeString(t)
	return err == nil
}
