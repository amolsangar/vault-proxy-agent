package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/justinas/alice"
	vault_proxy "github.com/zendesk/vault-proxy/pkg/vault-proxy"
)

// Entrypoint of program.
func main() {
	defaultAddress := fmt.Sprintf("%s:%d", vault_proxy.PROXY_ADDR, vault_proxy.PROXY_PORT)

	// `flag` Enables CLI override of proxy address / port -- e.g.: go run . -addr "127.0.0.1:8888"
	var proxyAddress = flag.String("addr", defaultAddress, "The addr of the application.")
	flag.Parse()

	// Vault Cache
	vaultCache := vault_proxy.NewVaultCache()

	// Parse Headers
	parseHeader := vault_proxy.NewParseHeader()

	// Vault Agent
	agent := vault_proxy.NewVaultAgent(*proxyAddress, vaultCache)

	// Rate Limiter
	rateLimiter := vault_proxy.NewTokenRateLimiter(vault_proxy.BURST_LIMIT_PER_SECOND, vault_proxy.RATE_LIMIT_PER_MINUTE, vault_proxy.RATE_LIMITER_BUCKET_SIZE, vaultCache)

	// Vault Proxy
	proxyHandler := vault_proxy.NewVaultProxy(vault_proxy.VAULT_ADDR, vault_proxy.VAULT_PORT, vaultCache)

	// Chain Middlewares/Handlers
	chain := alice.New(parseHeader.ParseHeaderHandler, agent.VaultAgentHandler, rateLimiter.RateLimitHandler).Then(proxyHandler)

	log.Println("Starting proxy server on", *proxyAddress)
	if err := http.ListenAndServe(*proxyAddress, chain); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
