package csrftoken

import (
	"crypto/subtle"
	"sync"
	"time"

	"github.com/Activer007/Gokapi/internal/helper"
)

var tokens = make(map[string]csrfToken)
var mutex sync.Mutex
var cleanupOnce sync.Once

const ttl = 5 * time.Minute

const (
	TypeLogin    = iota
	TypeApiToken // currently not used
	TypePasswordChange
)

type csrfToken struct {
	Type    int
	Expiry  int64
	Binding string
}

func Generate(tokenType int) string {
	return generate(tokenType, "")
}

// GenerateBound issues a token for one browser context. Empty bindings fail closed.
func GenerateBound(tokenType int, binding string) string {
	if binding == "" {
		return ""
	}
	return generate(tokenType, binding)
}

func generate(tokenType int, binding string) string {
	token := helper.GenerateRandomString(20)
	mutex.Lock()
	tokens[token] = csrfToken{
		Type:    tokenType,
		Expiry:  time.Now().Add(ttl).Unix(),
		Binding: binding,
	}
	mutex.Unlock()

	cleanupOnce.Do(func() {
		go cleanup(true)
	})
	return token
}

func IsValid(tokenType int, tokenId string) bool {
	return isValid(tokenType, tokenId, "")
}

func IsValidBound(tokenType int, tokenId, binding string) bool {
	if binding == "" {
		return false
	}
	return isValid(tokenType, tokenId, binding)
}

func isValid(tokenType int, tokenId, binding string) bool {
	mutex.Lock()
	defer mutex.Unlock()
	token, ok := tokens[tokenId]
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token.Binding), []byte(binding)) != 1 {
		return false
	}
	delete(tokens, tokenId) //always deletes token, even if it was wrong type
	if token.Type != tokenType {
		return false
	}
	return token.Expiry > time.Now().Unix()
}

func cleanup(periodic bool) {
	mutex.Lock()
	for tokenId, token := range tokens {
		if token.Expiry < time.Now().Unix() {
			delete(tokens, tokenId)
		}
	}
	mutex.Unlock()
	if periodic {
		time.Sleep(time.Hour)
		go cleanup(true)
	}

}
