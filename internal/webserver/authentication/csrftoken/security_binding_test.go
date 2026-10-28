package csrftoken

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSEC023Binding(t *testing.T) {
	resetStateBlockCleanup()
	if GenerateBound(TypeLogin, "") != "" {
		t.Fatal("empty binding issued a token")
	}
	token := GenerateBound(TypeLogin, "browser-a")
	if IsValid(TypeLogin, token) || IsValidBound(TypeLogin, token, "") || IsValidBound(TypeLogin, token, "browser-b") {
		t.Fatal("token escaped its browser binding")
	}
	if !IsValidBound(TypeLogin, token, "browser-a") || IsValidBound(TypeLogin, token, "browser-a") {
		t.Fatal("bound token must work exactly once")
	}
	unbound := Generate(TypeLogin)
	if IsValidBound(TypeLogin, unbound, "browser-a") {
		t.Fatal("legacy token accepted by bound validation")
	}
	token = GenerateBound(TypeLogin, "browser-a")
	if IsValidBound(TypePasswordChange, token, "browser-a") || IsValidBound(TypeLogin, token, "browser-a") {
		t.Fatal("wrong type must fail and consume the token")
	}
	token = GenerateBound(TypeLogin, "browser-a")
	mutex.Lock()
	value := tokens[token]
	value.Expiry = time.Now().Unix()
	tokens[token] = value
	mutex.Unlock()
	if IsValidBound(TypeLogin, token, "browser-a") {
		t.Fatal("expiry boundary accepted")
	}
}

func TestSEC023BoundConcurrentConsume(t *testing.T) {
	resetStateBlockCleanup()
	token := GenerateBound(TypeLogin, "browser-a")
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			<-start
			if IsValidBound(TypeLogin, token, "browser-a") {
				successes.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumers=%d", successes.Load())
	}
}
