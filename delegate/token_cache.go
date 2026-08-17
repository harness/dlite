package delegate

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

var (
	audience       = "audience"
	issuer         = "issuer"
	expirationTime = 20 * time.Minute
)

type TokenCache struct {
	id         string
	secret     string
	secretHash string
	expiry     time.Duration
	c          *cache.Cache
}

// NewTokenCache creates a token cache which creates a new token
// after the expiry time is over
func NewTokenCache(id, secret string) *TokenCache {
	// purge expired tokens from the cache at expirationTime/3 intervals
	c := cache.New(cache.DefaultExpiration, expirationTime/3)
	secret = normalizeSecret(secret)
	return &TokenCache{
		id:         id,
		secret:     secret,
		secretHash: hashSecret(secret),
		expiry:     expirationTime,
		c:          c,
	}
}

// GetTokenHash returns the SHA-256 of the normalized account secret in the
// manager's HashUtils.calculateSha256 format (BigInteger hex, leading zero
// nibbles stripped). The manager indexes delegate tokens by this hash, so
// sending it lets the manager look up the token directly instead of
// iterating over all active tokens of the account.
func (t *TokenCache) GetTokenHash() string {
	return t.secretHash
}

// normalizeSecret mirrors DelegateTokenUtils.getDecodedTokenString on the
// agent side: a token copied from the Harness UI is base64-encoded and Docker
// delegates receive it undecoded, while kubernetes secret injection already
// delivers the decoded hex token. If the value base64-decodes to a 32-char
// hex string, the decoded form is the actual delegate token — the manager
// stores tokenHash as calculateSha256 of that decoded hex token and derives
// the JWE AES key from it — so both hashing and token minting must use the
// decoded form.
func normalizeSecret(secret string) string {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, secret)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := enc.DecodeString(compact); err == nil {
			if s := strings.TrimSpace(string(decoded)); isHexToken(s) {
				return s
			}
		}
	}
	return secret
}

// isHexToken reports whether s is a 32-char hex string, matching the
// manager's isHexDecimalString check (delegate tokens are AES-128 keys
// rendered as 32 hex chars).
func isHexToken(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// hashSecret mirrors the manager's HashUtils.calculateSha256, which encodes
// the digest via Java's BigInteger.toString(16): leading zero nibbles are
// stripped (then left-padded back to a minimum of 32 chars), so stored
// DelegateToken.tokenHash values can be shorter than 64 hex chars. The
// manager's indexed token lookup has no fallback on a miss, so the encoding
// must match that format exactly.
func hashSecret(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	h := strings.TrimLeft(hex.EncodeToString(sum[:]), "0")
	for len(h) < 32 {
		h = "0" + h
	}
	return h
}

// Get returns the value of the account token.
// If the token is cached, it returns from there. Otherwise
// it creates a new token with a new expiration time.
func (t *TokenCache) Get() (string, error) {
	tv, found := t.c.Get(t.id)
	if found {
		return tv.(string), nil
	}
	logrus.WithField("id", t.id).Infoln("refreshing token")
	token, err := Token(audience, issuer, t.id, t.secret, t.expiry)
	if err != nil {
		return "", err
	}
	// refresh token before the expiration time to give some buffer
	t.c.Set(t.id, token, t.expiry/2)
	return token, nil
}
