package vault_proxy

// Configurable Constants

const VAULT_ADDR = "127.0.0.1"
const VAULT_PORT = 8080
const PROXY_ADDR = "127.0.0.1"
const PROXY_PORT = 8001
const VAULT_CACHE_DEFAULT_EXPIRATION = 30 // responses are cached for 60 seconds.
const VAULT_CACHE_PURGE_FREQUENCY = 30    // force purge all expired records every 1.5 minutes to prevent unnecessary memory bloat

// Any URL that contains 1 of these subpaths will be eligible for caching.
var CACHEABLE_SUBPATHS = [...]string{
	"/v1/secret/data",
}

// Any request of the following method types will be ignored
// DELETE for deleting key values
// POST for create/update key values
// https://www.vaultproject.io/api-docs/secret/kv/kv-v1
var METHODS_TO_IGNORE = [...]string{
	"DELETE",
	"POST",
	"PATCH",
}

// Rate limiters should be purged at a much higher rate than vault cache
// since deleting rate limiters resets API tracking
const RATE_LIMITER_DEFAULT_EXPIRATION = 60 // rate-limiters are cached for 120 seconds.
const RATE_LIMITER_PURGE_FREQUENCY = 60    // purges all unused limiters after default expiration time

const RATELIMITING_HASHING_KEY_PREFIX = "umtmynuxphgogwcickiyyongcdmpldofpqufkvdmckasamrtzk"
const RATELIMITING_HASHING_KEY_SUFFIX = "fiamhqbicxrgcrfvirlkdxmxzdbxoeojhkfffjsqycxizncojv"

// Rate limiting
const BURST_LIMIT_PER_SECOND = 2   // Burst requests allowed per second
const RATE_LIMIT_PER_MINUTE = 5    // Number of requests allowed per minute
const RATE_LIMITER_BUCKET_SIZE = 5 // Max requests allowed in a time frame

const CACHE_SIZE = 2
const RATE_LIMITER_CACHE_SIZE = 2

const VAULT_CONFIG_CHECK_FREQUENCY = 5 // Checks vault configuration every 5 seconds

const VAULT_ROOT_TOKEN = "hvs.0m0imDAXB9AVSkr0LaeCbKNa"

const AGENT_VAULT_PORT_DIFF = 1000
const AGENT_REQUEST_TIMEOUT = 2

// Static Constants

const VAULT_TOKEN_HEADER = "X-Vault-Token"
const VAULT_NAMESPACE_HEADER = "X-Vault-Namespace"
