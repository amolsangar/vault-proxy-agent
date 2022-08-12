ui = true

listener "tcp" {
  address     = "0.0.0.0:8080"
  tls_disable = "true"
}

storage "raft" {
  path = "./raft_data"
}

api_addr      = "http://localhost:8080"
cluster_addr  = "https://localhost:9001"