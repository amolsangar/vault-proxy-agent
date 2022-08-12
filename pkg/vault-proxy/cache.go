package vault_proxy

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Cache
type vaultCache struct {
	lock           sync.RWMutex
	cache          map[string]*cachedResponse
	lastCachePurge int64 // Millis since epoch of last cache purge; Used by purgeOldCacheEntries()
}

// Should ALWAYS be used as the "constructor" for the vaultCache. Initializes cache.
func NewVaultCache() *vaultCache {
	vc := new(vaultCache)
	vc.cache = make(map[string]*cachedResponse, CACHE_SIZE)
	vc.lastCachePurge = time.Now().UnixMilli()
	return vc
}

// Reads data from cache
func (c *vaultCache) getFromCache(key string) (*cachedResponse, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	d, keyExists := c.cache[key]
	return d, keyExists
}

// Writes data to cache
func (c *vaultCache) setInCache(key string, response *http.Response) {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Checks if cache is full and removes item using LRU policy
	c.purgeLruCacheEntries()

	c.cache[key] = newCachedResponse(response)
}

// Deletes data to cache
func (c *vaultCache) removeFromCache(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.cache, key)
}

// Parses relevant data from the request object as needed for caching.
func (c *vaultCache) parseVaultRequest(request *http.Request) (string, string, string) {
	return request.Header.Get(VAULT_TOKEN_HEADER),
		request.Header.Get(VAULT_NAMESPACE_HEADER),
		request.URL.Path
}

// Purges 1/4 of the least recently used items from cache when full
func (c *vaultCache) purgeLruCacheEntries() {
	if len(c.cache) >= CACHE_SIZE {
		log.Printf("Purging vault cache because its full.")
		// Get cache keys
		keys := make([]string, 0, len(c.cache))
		for key := range c.cache {
			keys = append(keys, key)
		}

		// Sort by cache expiration
		sort.SliceStable(keys, func(i, j int) bool {
			return c.cache[keys[i]].lastUsed < c.cache[keys[j]].lastUsed
		})

		for i, k := range keys {
			delete(c.cache, k)
			if i >= len(c.cache)/4 {
				break
			}
		}
	}
}

// Purges expired items from cache on configured VAULT_CACHE_PURGE_FREQUENCY
func (c *vaultCache) purgeOldCacheEntries() {
	if time.Now().UnixMilli()-VAULT_CACHE_PURGE_FREQUENCY*1000 > c.lastCachePurge {
		// Lock cache so purge is not interrupted.
		c.lock.Lock()
		defer c.lock.Unlock()

		log.Printf("Purging cache. It has not been purged in %d seconds.", VAULT_CACHE_PURGE_FREQUENCY)
		for key, cachedResponse := range c.cache {
			if cachedResponse.isExpired() {
				log.Printf("Expired key detected, deleting %s from cache.", key)
				delete(c.cache, key)
			}
		}

		c.lastCachePurge = time.Now().UnixMilli()
	}
}

// Converts request details into a hashed cache key
func (c *vaultCache) getMD5HashedCacheKey(request *http.Request) string {
	token, namespace, path := c.parseVaultRequest(request)
	vaultHashKey := fmt.Sprintf("%s-%s-%s", token, path, namespace)

	// Generate MD5 hash from vault token/path/namespace
	hasher := md5.New()
	hasher.Write([]byte(vaultHashKey))
	md5Hex := hex.EncodeToString(hasher.Sum(nil))

	return md5Hex
}

// Retrieves cached response if present, otherwise returns error
func (c *vaultCache) getCachedResponse(request *http.Request) (*http.Response, error) {
	c.purgeOldCacheEntries()
	var err error = nil
	var response *http.Response = &http.Response{}
	cacheKey := c.getMD5HashedCacheKey(request)
	cachedResponse, keyExists := c.getFromCache(cacheKey)
	if keyExists && !cachedResponse.isExpired() {
		// Update last access time to avoid LRU cache purging
		cachedResponse.lastUsed = time.Now().UnixMilli()

		log.Printf("CACHE HIT: Key: %s found in cache, returning cached response!", cacheKey)
		response = cachedResponse.getResponse()
	} else {
		err = errors.New("key not found in cache")
	}

	// Need a log.debug level -- hopefully there is an internal lib for this stuff :)
	// log.Printf("Returning response: %+v", response)
	return response, err
}

// Refreshes the cache by fetching token from Vault
func (c *vaultCache) refreshCache(request *http.Request, refresher func() (*http.Response, error)) (*http.Response, error) {
	var err error = nil
	var response *http.Response = &http.Response{}
	cacheKey := c.getMD5HashedCacheKey(request)

	log.Printf("CACHE MISS: Key: %s NOT found in cache or value is expired. Looking up....", cacheKey)
	response, err = refresher()
	if response.StatusCode == 200 {
		c.setInCache(cacheKey, response)
	}

	// Need a log.debug level -- hopefully there is an internal lib for this stuff :)
	// log.Printf("Returning response: %+v", response)
	return response, err
}
