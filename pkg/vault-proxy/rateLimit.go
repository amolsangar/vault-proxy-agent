package vault_proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter interface for overriding
type RateLimiter interface {
	Allow() bool
	Wait(context.Context) error
	Limit() rate.Limit
}

type multiLimiter struct {
	limiters []RateLimiter
}

// Visitor struct which holds the rate limiter for each
// visitor and the last time that it was used.
type visitor struct {
	limiter  *multiLimiter
	lastUsed int64
}

// Token Rate Limiter
type tokenRateLimiter struct {
	limiterCache          map[string]*visitor
	lock                  *sync.RWMutex
	burstLimitPerSec      int
	rateLimitPerMin       int
	rateLimiterBucketSize int
	lastRateLimiterPurge  int64 // Millis since epoch of last RateLimiter purge; Used by purgeTokenLimiters()
	vaultCache            *vaultCache
}

// Should ALWAYS be used as the "constructor" for the tokenRateLimiter. Initializes rate-limiting.
func NewTokenRateLimiter(burstPerSec int, ratePerMin int, limiterBucketSize int, cache *vaultCache) *tokenRateLimiter {
	return &tokenRateLimiter{
		limiterCache:          make(map[string]*visitor),
		lock:                  &sync.RWMutex{},
		burstLimitPerSec:      burstPerSec,
		rateLimitPerMin:       ratePerMin,
		rateLimiterBucketSize: limiterBucketSize,
		lastRateLimiterPurge:  time.Now().UnixMilli(),
		vaultCache:            cache,
	}
}

// Returns multilimiter
func MultiLimiter(limiters ...RateLimiter) *multiLimiter {
	byLimit := func(i, j int) bool {
		return limiters[i].Limit() < limiters[j].Limit()
	}

	sort.Slice(limiters, byLimit)
	return &multiLimiter{limiters: limiters}
}

// Consumes token and returns immedietly
func (l *multiLimiter) Allow() bool {
	for _, l := range l.limiters {
		if !l.Allow() {
			return false
		}
	}
	return true
}

// Consumes token and waits if the token is not present for use
func (l *multiLimiter) Wait(ctx context.Context) error {
	for _, l := range l.limiters {
		if err := l.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (l *multiLimiter) Limit() rate.Limit {
	return l.limiters[0].Limit()
}

// getFromLimiterCache returns the rate limiter for the provided token if it exists.
// Otherwise calls setInLimiterCache to add token to the map
func (l *tokenRateLimiter) getFromLimiterCache(token string) *multiLimiter {
	l.lock.RLock()

	visitor, exists := l.limiterCache[token]
	if !exists {
		l.lock.RUnlock()
		return l.setInLimiterCache(token)
	}
	visitor.lastUsed = time.Now().UnixMilli()
	l.lock.RUnlock()

	return visitor.limiter
}

// setInLimiterCache creates a new rate limiter and adds it to the limiterCache map,
// using the token as the key
func (l *tokenRateLimiter) setInLimiterCache(token string) *multiLimiter {
	l.lock.Lock()
	defer l.lock.Unlock()

	// Checks if rate-limiters cache is full and removes item using LRU policy
	l.purgeLruTokenLimiters()

	limiter := MultiLimiter(
		rate.NewLimiter(Per(int(l.burstLimitPerSec), time.Second), 1),                 // burst requests
		rate.NewLimiter(Per(l.rateLimitPerMin, time.Minute), l.rateLimiterBucketSize), // normal requests
	)
	l.limiterCache[token] = &visitor{limiter, time.Now().UnixMilli()}
	return limiter
}

func Per(eventCount int, duration time.Duration) rate.Limit {
	return rate.Every(duration / time.Duration(eventCount))
}

// Purges 1/4 of the least recently used items from rate-limiters cache when full
func (l *tokenRateLimiter) purgeLruTokenLimiters() {
	if len(l.limiterCache) >= RATE_LIMITER_CACHE_SIZE {
		log.Printf("Purging rate-limiters cache because its full.")

		// Get rate-limiters cache keys
		keys := make([]string, 0, len(l.limiterCache))
		for key := range l.limiterCache {
			keys = append(keys, key)
		}

		// Sort by rate-limiters lastUsed
		sort.SliceStable(keys, func(i, j int) bool {
			return l.limiterCache[keys[i]].lastUsed < l.limiterCache[keys[j]].lastUsed
		})

		// Delete 1/4 cache
		for i, k := range keys {
			delete(l.limiterCache, k)
			if i >= len(l.limiterCache)/4 {
				break
			}
		}
	}
}

// Purge rate-limiters every RATE_LIMITER_PURGE_FREQUENCY which hasn't been used
func (l *tokenRateLimiter) purgeTokenLimiters() {
	if time.Now().UnixMilli()-RATE_LIMITER_PURGE_FREQUENCY*1000 > l.lastRateLimiterPurge {
		log.Printf("Purging rate-limiters. It has not been purged in %d seconds.", RATE_LIMITER_PURGE_FREQUENCY)
		l.lock.Lock()
		defer l.lock.Unlock()

		for token, v := range l.limiterCache {
			if time.Now().UnixMilli() > v.lastUsed+RATE_LIMITER_DEFAULT_EXPIRATION*1000 {
				delete(l.limiterCache, token)
			}
		}
		l.lastRateLimiterPurge = time.Now().UnixMilli()
	}
}

// Rate-limits the incoming requests and checks for cached responses before sending it to vaultProxy
func (l *tokenRateLimiter) RateLimitHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		l.purgeTokenLimiters()

		rateLimitingKey := request.Context().Value(parsedHeaderContextKey).(*parseHeader).GetLimiterCacheKey()
		isPathCacheable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsPathCacheable()
		isRequestIgnorable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsRequestIgnorable()

		log.Printf("Rate-Limit Check: STARTED: Hashkey: %s \n", rateLimitingKey)
		limiter := l.getFromLimiterCache(rateLimitingKey)
		// Important that this is called before checking cache,
		// in order to consume one token for rate-limiting
		isAllowed := limiter.Allow()

		// Read request - Check if response is already cached
		if isPathCacheable && !isRequestIgnorable {
			log.Printf("Rate-Limit Check: Checking Cache\n")
			response, err := l.vaultCache.getCachedResponse(request)
			if err != nil {
				log.Printf("Rate-Limit Check: CACHE-MISS: Hashkey: %s \n", rateLimitingKey)
			} else {
				defer response.Body.Close()

				copyHeaders(writer.Header(), response.Header)
				writer.WriteHeader(response.StatusCode)
				_, err = io.Copy(writer, response.Body)

				if err != nil {
					log.Fatal("Error copying response from cache.", err)
				}
				return
			}
		}

		// Return 429 error
		if !isAllowed {
			log.Printf("Rate-Limit Check: TOO MANY REQUESTS: Hashkey: %s \n", rateLimitingKey)
			http.Error(writer, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		log.Printf("Rate-Limit Check: PASSED: Hashkey: %s \n", rateLimitingKey)
		next.ServeHTTP(writer, request)
	})
}
