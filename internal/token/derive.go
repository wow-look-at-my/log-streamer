package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrNoKey reports a keyless derivation, which public metadata gives away.
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

// groupPrefix keeps an index from ever colliding with a log.
const groupPrefix = "group"

// DeriveGroup returns the token indexing every stream of a context, which a
// watcher computes from the key and the run alone.
func DeriveGroup(key, context string) (string, error) {
	if context == "" {
		return "", ErrNoContext
	}
	return Derive(key, groupPrefix+"/"+context)
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
