package vault_proxy

import "net/http"

// Copies headers between http.Header objects.
func copyHeaders(dst http.Header, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
