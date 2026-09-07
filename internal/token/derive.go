package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrNoKey reports a keyless derivation, whose token the public run metadata
// would give away.
var ErrNoKey = errors.New("no derivation key")

// ErrNoContext reports a derivation with nothing to name the stream.
var ErrNoContext = errors.New("no derivation context")

// Derive returns the token naming a stream, as HMAC-SHA256 over the context.
// Both the writer and the reader compute it offline from facts they already
// share, so the token never has to travel through the log itself.
func Derive(key, context string) (string, error) {
	if key == "" {
		return "", ErrNoKey
	}
	if context == "" {
		return "", ErrNoContext
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(context))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Context joins the parts naming a stream, skipping empties so a missing
// optional part cannot silently shift the others into different positions.
func Context(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "/")
}
