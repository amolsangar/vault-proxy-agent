# Vault Proxy Agent

Runs as an independent process on a Vault host that transparently proxies requests to the vault API but 
caches ingress requests for N seconds based on criteria. Rate-limiting is also set in place for managing the amount of ingress requests.

The entire HTTP response entity is cached and returned to the user. To prevent cache mining by brute force, the
cache KEY is a combination of these request properties:

- *Vault Token*
- *Path to K/V secret*
- *Vault Namespace*

Default configurations can be managed in `config.go`. 

## To Run:

Start vault:

`vault server -dev`

Start vault as a raft cluster:

`vault server -config=raft_config/config.hcl`

**Note the root token, use in CURL below**

Add 1-N K/V Secrets through Vault UI: http://localhost:8200

Run proxy locally:

`go run cmd/main.go -addr "127.0.0.1:8001"`

`go run cmd/main.go -addr "127.0.0.1:8002"`

`go run cmd/main.go -addr "127.0.0.1:8003"`

Sample request (note port of 8001 which targets the proxy and not vault)

```bash
curl -vv \
--header "X-Vault-Token: YOUR_ROOT_TOKEN" \
"http://127.0.0.1:8001/v1/secret/data/test/one/two/three?version=1"
```

### Get raft configuration
```bash
curl \
--header "X-Vault-Token: YOUR_ROOT_TOKEN" \
--request GET \
"http://127.0.0.1:8080/v1/sys/storage/raft/configuration"
```

### Create Token
```bash
curl \
--header "X-Vault-Token: YOUR_ROOT_TOKEN" \
--request POST \
--data @payload.json \
"http://127.0.0.1:8001/v1/auth/token/create"
```

```json
// payload.json
{
  "policies": ["root"],
  "meta": {
    "user": "testUser"
  },
  "ttl": "1h",
  "renewable": true
}
```

Sample Vault CLI command:

`vault kv get secret/test/one/two/three`
