package vault_proxy

import (
	"encoding/json"
	"hash/fnv"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spaolacci/murmur3"
)

// Vault Servers
type Server struct {
	Address         string `json:"address"`
	Leader          bool   `json:"leader"`
	NodeId          string `json:"node_id"`
	ProtocolVersion string `json:"protocol_version"`
	Voter           bool   `json:"voter"`
}

// Vault Config Response
type VaultConfigResponse struct {
	RequestId     string `json:"request_id"`
	LeaseId       string `json:"lease_id"`
	LeaseDuration int    `json:"lease_duration"`
	Renewable     bool   `json:"renewable"`
	Data          struct {
		Config struct {
			Index   int `json:"index"`
			Servers []Server
		}
	}
	Warnings interface{} `json:"warnings,omitempty"`
}

// Vault Agent
type vaultAgent struct {
	vaultConfigResponse VaultConfigResponse
	agentRoutingTable   map[int]string
	lastConfigCheck     int64 // Millis since epoch of last vault config check;
	lock                sync.RWMutex
	myAddress           string
	vaultCache          *vaultCache
}

// Should ALWAYS be used as the "constructor" for the vaultAgent. Initializes rate-limiting.
func NewVaultAgent(proxyAddress string, vaultCache *vaultCache) *vaultAgent {
	return &vaultAgent{
		agentRoutingTable: make(map[int]string),
		lastConfigCheck:   0, // Force config check on startup
		myAddress:         proxyAddress,
		vaultCache:        vaultCache,
	}
}

// Add mock servers for local development
func (a *vaultAgent) addMockServers(responseObject VaultConfigResponse) VaultConfigResponse {
	mockServer1 := Server{
		Address:         "127.0.0.1:9001",
		Leader:          false,
		NodeId:          "raft2",
		ProtocolVersion: "\u0003",
		Voter:           true,
	}

	mockServer2 := Server{
		Address:         "127.0.0.1:9001",
		Leader:          false,
		NodeId:          "raft3",
		ProtocolVersion: "\u0003",
		Voter:           true,
	}

	responseObject.Data.Config.Servers = append(responseObject.Data.Config.Servers, mockServer1)
	responseObject.Data.Config.Servers = append(responseObject.Data.Config.Servers, mockServer2)

	// log.Printf("Mock Servers Added: API Response as struct %+v\n", responseObject.Data.Config.Servers)
	return responseObject
}

// Get raft peer details
func (a *vaultAgent) getVaultConfigDetails() {
	if time.Now().UnixMilli()-VAULT_CONFIG_CHECK_FREQUENCY*1000 > a.lastConfigCheck {
		a.lock.Lock()
		defer a.lock.Unlock()
		addr := "http://" + VAULT_ADDR + ":" + strconv.Itoa(VAULT_PORT) + "/v1/sys/storage/raft/configuration"
		client := &http.Client{}
		req, err := http.NewRequest("GET", addr, nil)
		if err != nil {
			log.Print(err.Error())
		}
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("X-Vault-Token", VAULT_ROOT_TOKEN)
		resp, err := client.Do(req)
		if err != nil {
			log.Print(err.Error())
		}
		defer resp.Body.Close()

		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Print(err.Error())
		}

		var responseObject VaultConfigResponse
		json.Unmarshal(bodyBytes, &responseObject)

		// REMOVE THIS BEFORE DEPLOYMENT
		a.vaultConfigResponse = a.addMockServers(responseObject)

		// Changes ports from Agent use
		a.changePortMapping()

		// Sort and update the routing table
		a.sortByNodeId()

		a.lastConfigCheck = time.Now().UnixMilli()
	}
}

// Replaces vault ports with Agent port numbers
// Agent Port Logic: port - AGENT_VAULT_PORT_DIFF
// Ex - port=8444, AGENT_VAULT_PORT_DIFF=1000
// Agent Port = 8444 - 1000 = 7444
func (a *vaultAgent) changePortMapping() {
	for i, server := range a.vaultConfigResponse.Data.Config.Servers {
		serverAddress := server.Address
		addrPort := strings.Split(serverAddress, ":")

		// string to int
		port, err := strconv.Atoi(addrPort[1])
		if err != nil {
			log.Print(err.Error())
		}

		// For local development only
		addrPort[1] = strconv.Itoa(port - AGENT_VAULT_PORT_DIFF + i)

		// For other environments
		// addrPort[1] = int(addrPort[1]) - AGENT_VAULT_PORT_DIFF

		a.vaultConfigResponse.Data.Config.Servers[i].Address = addrPort[0] + ":" + addrPort[1]
	}
}

func (a *vaultAgent) sortByNodeId() {
	nodes := a.vaultConfigResponse.Data.Config.Servers

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeId < nodes[j].NodeId
	})
	log.Println("Sorted Nodes :", nodes)

	for i, node := range nodes {
		a.agentRoutingTable[i] = node.Address
	}
	log.Println("Agent Routing Table:", a.agentRoutingTable)
}

// https://softwareengineering.stackexchange.com/questions/49550/which-hashing-algorithm-is-best-for-uniqueness-and-speed
// String to integer Murmur3 hash function
func hash(s string) uint32 {
	h := murmur3.Sum32WithSeed([]byte(s), 0x1234ABCD)
	return h
}

// FNV hash
func fnvHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// Gets the routing server address
func (a *vaultAgent) GetRoutingServer(request *http.Request) string {
	token := request.Header.Get(VAULT_TOKEN_HEADER)
	server_no := int(hash(token)) % len(a.vaultConfigResponse.Data.Config.Servers)
	return a.agentRoutingTable[server_no]
}

// Vault Agent Handler - Routes request to other agents
// if routing address is different from running server's address
// else runs on the same agent
func (a *vaultAgent) VaultAgentHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Fetches and stores the current vault configuration every VAULT_CONFIG_CHECK_FREQUENCY seconds
		a.getVaultConfigDetails()

		// If routingServer address is different, then forward the request to routingServer agent
		// else run the request on the same agent
		path := request.URL.Path
		method := request.Method
		myAddress := a.myAddress
		log.Printf("My server address: %s", myAddress)

		isPathCacheable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsPathCacheable()
		isRequestIgnorable := request.Context().Value(parsedHeaderContextKey).(*parseHeader).IsRequestIgnorable()

		// Forward the request to Vault if path is not cacheable
		if isPathCacheable {
			// create/update/delete request - Invalidate cache
			if isRequestIgnorable {
				log.Printf("Invalidating cache: Method %s Path: %s", method, path)
				key := request.Context().Value(parsedHeaderContextKey).(*parseHeader).GetVaultCacheKey()
				a.vaultCache.removeFromCache(key)
			} else {
				// Gets the routing server address
				routingServer := a.GetRoutingServer(request)

				// Read request - route to agent
				if routingServer != myAddress {
					// Request URI must be dumped, it can't be set in client requests.
					// http://golang.org/src/pkg/net/http/client.go
					request.RequestURI = ""
					request.URL.Scheme = "http"
					request.URL.Host = routingServer

					client := &http.Client{Timeout: AGENT_REQUEST_TIMEOUT * time.Second}
					response := new(http.Response)
					var err error = nil

					log.Printf("Routing to Agent: %s Path: %s", routingServer, path)
					response, err = client.Do(request)

					if err != nil {
						// if there is an error check if its a timeout error
						if e, ok := err.(net.Error); ok && e.Timeout() {
							// timeout error
							log.Printf("Agent to request timed out: Processing request on the same Agent")
						} else {
							// Todo: this should throw an alert in Datadog.
							// http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
							log.Printf("Error from Agent: %s %v", routingServer, err)
							log.Printf("Running on the same agent due to connection error: %s Path: %s", myAddress, path)
						}
					} else {
						defer response.Body.Close()

						copyHeaders(writer.Header(), response.Header)
						writer.WriteHeader(response.StatusCode)
						_, err = io.Copy(writer, response.Body)

						if err != nil {
							log.Fatal("Error copying response from agent.", err)
						}
						return
					}
				} else {
					log.Printf("Running on the same agent: %s Path: %s", myAddress, path)
				}
			}
		}

		log.Printf("Agent work done!")
		next.ServeHTTP(writer, request)
	})
}
