package vault_proxy

import (
	"io"
	"net/http"
	"strings"
	"time"
)

type cachedResponse struct {
	response *http.Response
	bodyData string
	expires  int64
	lastUsed int64
}

// Returns the http.Response object that is cached and rewrites the stored Body to the Body stream.
func (cr *cachedResponse) getResponse() *http.Response {
	readerCloser := io.NopCloser(strings.NewReader(cr.bodyData))
	cr.response.Body = readerCloser

	return cr.response
}

// Returns `true` if the cached entry is expired.
func (cr *cachedResponse) isExpired() bool {
	return time.Now().UnixMilli() > cr.expires
}

// Should ALWAYS be used as the 'constructor' to this struct. Will properly initialize this instance of the struct.
func newCachedResponse(response *http.Response) *cachedResponse {
	// Copy response to buffer
	buffer := new(strings.Builder)
	io.Copy(buffer, response.Body)
	body := buffer.String()

	// Write buffer back to response after copy so it's fresh
	readerCloser := io.NopCloser(strings.NewReader(body))
	response.Body = readerCloser

	expires := time.Now().UnixMilli() + VAULT_CACHE_DEFAULT_EXPIRATION*1000
	lastUsed := time.Now().UnixMilli()

	return &cachedResponse{
		response,
		body,
		expires,
		lastUsed,
	}
}
