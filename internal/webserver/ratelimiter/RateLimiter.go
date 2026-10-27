package ratelimiter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/logging"
	"golang.org/x/time/rate"
)

var newUuidLimiter = newLimiter()
var failedLoginLimiter = newLimiter()
var failedIdLimiter = newLimiter()
var failedDownloadPasswordLimiter = newLimiter()
var failedApiKeyLimiter = newLimiter()
var failedSetupLogin = newLimiter()

// isUnitTest must be false and is only set to true for running test units
// If true, rate limiting is disabled
var isUnitTest = false

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// SetUnitTestMode disables all rate limiting
// This is only used for running unit tests
func SetUnitTestMode(enabled bool) {
	fmt.Println("Rate limiting disabled for unit tests")
	isUnitTest = enabled
}

type store struct {
	mu          sync.Mutex
	limiters    map[string]*limiterEntry
	cleanupOnce sync.Once
}

func newLimiter() *store {
	return &store{
		limiters: make(map[string]*limiterEntry),
	}
}

// WaitOnLogin blocks the current goroutine until the rate limiter allows a request
// Three attempts without limiting, thereafter one attempt every 3 seconds
func WaitOnLogin(ctx context.Context, ip string) error {
	return failedLoginLimiter.Get(ip, 1, 9).WaitN(ctx, 3)
}

// WaitOnSetupLogin blocks the current goroutine until the rate limiter allows a request
// Three attempts without limiting, thereafter one attempt every 1 second
func WaitOnSetupLogin(ctx context.Context) error {
	return failedSetupLogin.Get("setup", 1, 15).WaitN(ctx, 1)
}

// WaitOnApiAuthentication blocks the current goroutine until the rate limiter allows a request
// 200 attempts without limiting, thereafter one attempt every second
func WaitOnApiAuthentication(ctx context.Context, ip string) error {
	return failedApiKeyLimiter.Get(ip, 1, 200).WaitN(ctx, 1)
}

// WaitOnDownloadPassword blocks the current goroutine until the rate limiter allows a request
// Ten attempts without limiting, thereafter one attempt every 2 seconds
func WaitOnDownloadPassword(ctx context.Context, ip string) error {
	return failedDownloadPasswordLimiter.Get(ip, 1, 20).WaitN(ctx, 2)
}

// WaitOnFailedId blocks the current goroutine until the rate limiter allows a request
// Ten failed attempts without limiting, thereafter one attempt every second
func WaitOnFailedId(r *http.Request) error {
	ip := logging.GetIpAddress(r)
	return failedIdLimiter.Get(ip, 1, 10).Wait(r.Context())
}

// IsAllowedNewUuid returns true if a new uuid is not rate-limited
// Four initial requests are allowed without rate limiting, thereafter one every second
func IsAllowedNewUuid(key string) bool {
	return newUuidLimiter.Get(key, 1, 4).Allow()
}

// Get returns the rate limiter for the given key
func (s *store) Get(key string, r rate.Limit, burst int) *rate.Limiter {
	if isUnitTest {
		return rate.NewLimiter(r, burst)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.limiters[key]
	if !ok {
		e = &limiterEntry{
			limiter: rate.NewLimiter(r, burst),
		}
	}

	e.lastSeen = time.Now()
	s.limiters[key] = e
	s.StartCleanup(12 * time.Hour)
	return e.limiter
}

// StartCleanup starts a goroutine that continuously cleans up old entries from the store
func (s *store) StartCleanup(maxIdle time.Duration) {
	s.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			for range ticker.C {
				now := time.Now()
				s.mu.Lock()
				for k, v := range s.limiters {
					if now.Sub(v.lastSeen) > maxIdle {
						delete(s.limiters, k)
					}
				}
				s.mu.Unlock()
			}
		}()
	})
}
