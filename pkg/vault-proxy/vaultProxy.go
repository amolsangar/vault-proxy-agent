package vault_proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

// Proxies
type vaultProxy struct {
	vaultAddr  string
	vaultPort  int16
	vaultCache *vaultCache
}

// Should ALWAYS be used as the "constructor" for the vaultProxy. Initializes cache and important defaults.
func NewVaultProxy(vaultAddr string, vaultPort int16, vaultCache *vaultCache) *vaultProxy {
	vp := new(vaultProxy)
	vp.vaultAddr = vaultAddr
	vp.vaultPort = vaultPort
	vp.vaultCache = vaultCache
	return vp
}

// Serves all HTTP traffic.
func (p *vaultProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// Request URI must be dumped, it can't be set in client requests.
	// http://golang.org/src/pkg/net/http/client.go
	request.RequestURI = ""
	request.URL.Scheme = "http"
	request.URL.Host = fmt.Sprintf("%s:%d", p.vaultAddr, p.vaultPort)

	path := request.URL.Path
	method := request.Method

	client := &http.Client{}
	response := new(http.Response)
	var err error = nil

	isPathCacheable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsPathCacheable()
	isRequestIgnorable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsRequestIgnorable()

	// Read request - cache it
	if isPathCacheable && !isRequestIgnorable {
		log.Printf("Method: %s Path: %s is cachable!", method, path)
		response, err = p.vaultCache.refreshCache(request, func() (*http.Response, error) {
			return client.Do(request)
		})

		if err != nil {
			// Todo: this should throw an alert in Datadog.
			http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
			log.Print("CacheableRequestError: ", err)
		}
	} else {
		log.Printf("Method: %s Path: %s is not cacheable, proxying without cache...", method, path)
		response, err = client.Do(request)

		if err != nil {
			// Todo: this should throw an alert in Datadog.
			http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
			log.Print("UncacheableRequestError: ", err)
		}
	}

	defer response.Body.Close()

	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, err = io.Copy(writer, response.Body)

	if err != nil {
		log.Fatal("Error copying response from cache.", err)
	}
}
