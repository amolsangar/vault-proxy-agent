# vault-proxy-agent
Runs as an independent process on a Vault host that transparently proxies requests to the vault API but caches ingress requests for N seconds based on criteria. Rate-limiting is also set in place for managing the amount of ingress requests.
