package delegate

import (
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

var (
	audience       = "audience"
	issuer         = "issuer"
	expirationTime = 10 * time.Minute // token gets refreshed every 10 minutes
)

type tokenCache struct {
	id     string
	secret string
	expiry time.Duration
	c      *cache.Cache
}

// NewTokenCache creates a token cache which creates a new token
// after the expiry time is over
func NewTokenCache(id, secret string) *tokenCache {
	c := cache.New(cache.DefaultExpiration, expirationTime)
	return &tokenCache{
		id:     id,
		secret: secret,
		expiry: expirationTime,
		c:      c,
	}
}

// Get returns the value of the account token.
// If the token is cached, it returns from there. Otherwise
// it creates a new token with a new expiration time.
func (t *tokenCache) Get() (string, error) {
	tv, found := t.c.Get(t.id)
	if found {
		return tv.(string), nil
	}
	logrus.WithField("id", t.id).Infoln("refreshing token")
	token, err := Token(audience, issuer, t.id, t.secret, t.expiry)
	if err != nil {
		return "", err
	}
	t.c.Set(t.id, token, t.expiry)
	return token, nil
}
