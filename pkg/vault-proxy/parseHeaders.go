package vault_proxy

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// Context Key
type contextKey string

const parsedHeaderContextKey contextKey = "parsedHeadersValues"

// Parse Header
type parseHeader struct {
	vaultCacheKey      string
	limiterCacheKey    string
	isPathCacheable    bool
	isRequestIgnorable bool
}

// Should ALWAYS be used as the "constructor" for the vaultCache. Initializes cache.
func NewParseHeader() *parseHeader {
	return &parseHeader{
		vaultCacheKey:      "",
		limiterCacheKey:    "",
		isPathCacheable:    false,
		isRequestIgnorable: true,
	}
}

// Get if path is cacheable
func (h *parseHeader) IsPathCacheable() bool {
	return h.isPathCacheable
}

// Get if request is ignorable
func (h *parseHeader) IsRequestIgnorable() bool {
	return h.isRequestIgnorable
}

// Get vault cache key
func (h *parseHeader) GetVaultCacheKey() string {
	return h.vaultCacheKey
}

// Get limiter cache key
func (h *parseHeader) GetLimiterCacheKey() string {
	return h.limiterCacheKey
}

// Returns 'true' if the request path is in the list of CACHEABLE_SUBPATHS provided in config.go
func (h *parseHeader) checkPathCacheable(path string) bool {
	for _, cacheableSubPath := range CACHEABLE_SUBPATHS {
		if strings.Contains(path, cacheableSubPath) {
			return true
		}
	}

	return false
}

// Returns 'true' if the request method is in the list of METHODS_TO_IGNORE provided in config.go
func (h *parseHeader) checkRequestIgnorable(method string) bool {
	for _, methodName := range METHODS_TO_IGNORE {
		if strings.Contains(method, methodName) {
			return true
		}
	}

	return false
}

// Parses relevant data from the request object as needed for caching.
func (h *parseHeader) parseVaultRequest(request *http.Request) (string, string, string) {
	return request.Header.Get(VAULT_TOKEN_HEADER),
		request.Header.Get(VAULT_NAMESPACE_HEADER),
		request.URL.Path
}

// Converts request details into a hashed cache key
func (h *parseHeader) getMD5HashedCacheKey(request *http.Request) string {
	token, namespace, path := h.parseVaultRequest(request)

	log.Printf("Fetching for: path %s \n", path)
	vaultHashKey := fmt.Sprintf("%s-%s-%s", token, path, namespace)

	// Generate MD5 hash from vault token/path/namespace
	hasher := md5.New()
	hasher.Write([]byte(vaultHashKey))
	md5Hex := hex.EncodeToString(hasher.Sum(nil))

	return md5Hex
}

// Converts request details into a hashed cache key
func (h *parseHeader) getMD5HashedLimiterKey(request *http.Request) string {
	token, _, _ := h.parseVaultRequest(request)

	// Generate MD5 hash from vault token
	rateLimitingHashKey := fmt.Sprintf("%s-%s-%s", RATELIMITING_HASHING_KEY_PREFIX, token, RATELIMITING_HASHING_KEY_SUFFIX)
	hasher := md5.New()
	hasher.Write([]byte(rateLimitingHashKey))
	md5Hex := hex.EncodeToString(hasher.Sum(nil))

	return md5Hex
}

// Parses header to get cache and limiter keys
func (h *parseHeader) ParseHeaderHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		h.vaultCacheKey = h.getMD5HashedCacheKey(request)
		h.limiterCacheKey = h.getMD5HashedLimiterKey(request)
		h.isPathCacheable = h.checkPathCacheable(request.URL.Path)
		h.isRequestIgnorable = h.checkRequestIgnorable(request.Method)
		ctx := context.WithValue(request.Context(), parsedHeaderContextKey, h)
		log.Printf("Headers Parsed: Vault cache key: %s Limiter cache key: %s \n", h.vaultCacheKey, h.limiterCacheKey)

		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}
