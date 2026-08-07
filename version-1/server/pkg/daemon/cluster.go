package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/DangLong/na-server-go/pkg/utils"
)

type JoinRequest struct {
	Password string `json:"password"`
	MyURL    string `json:"my_url"`
}

type JoinResponse struct {
	BindPort  int      `json:"bind_port"`
	VhostPort int      `json:"vhost_port"`
	Token     string   `json:"token"`
	Domains   string   `json:"domains"`
	Timestamp int64    `json:"timestamp"`
	Peers     []string `json:"peers"`
}

type SyncDomainRequest struct {
	Password  string `json:"password"`
	Domains   string `json:"domains"`
	Timestamp int64  `json:"timestamp"`
}

type SyncPeersRequest struct {
	Password string   `json:"password"`
	Peers    []string `json:"peers"`
}

func getLocalPeers() []string {
	content, err := os.ReadFile("/etc/nalink/peers.json")
	if err != nil {
		return []string{}
	}
	var peers []string
	json.Unmarshal(content, &peers)
	return peers
}

func saveLocalPeers(peers []string) {
	os.MkdirAll("/etc/nalink", 0755)
	data, _ := json.Marshal(peers)
	os.WriteFile("/etc/nalink/peers.json", data, 0644)
}

func getDomainTimestamp() int64 {
	content, err := os.ReadFile("/etc/nalink/domain_ts")
	if err != nil {
		return 0
	}
	ts, _ := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	return ts
}

func saveDomainTimestamp(ts int64) {
	os.MkdirAll("/etc/nalink", 0755)
	os.WriteFile("/etc/nalink/domain_ts", []byte(fmt.Sprintf("%d", ts)), 0644)
}

func ClusterJoinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req JoinRequest
	json.NewDecoder(r.Body).Decode(&req)

	if req.Password != secureToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Read configs
	info := getFRPSConfig()
	domainsBytes, _ := os.ReadFile("/etc/caddy/domains.txt")
	peers := getLocalPeers()

	// Add new peer if not exists
	found := false
	for _, p := range peers {
		if p == req.MyURL {
			found = true
			break
		}
	}
	if !found && req.MyURL != "" {
		peers = append(peers, req.MyURL)
		saveLocalPeers(peers)
		
		// Broadcast new peers to existing peers (except the new one)
		go broadcastPeers(peers, req.MyURL)
	}

	bPort, _ := strconv.Atoi(info.BindPort)
	vPort, _ := strconv.Atoi(info.VhostPort)

	res := JoinResponse{
		BindPort:  bPort,
		VhostPort: vPort,
		Token:     info.Token,
		Domains:   string(domainsBytes),
		Timestamp: getDomainTimestamp(),
		Peers:     peers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func ClusterSyncPeersHandler(w http.ResponseWriter, r *http.Request) {
	var req SyncPeersRequest
	json.NewDecoder(r.Body).Decode(&req)
	if req.Password != secureToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	saveLocalPeers(req.Peers)
	w.WriteHeader(http.StatusOK)
}

func ClusterSyncDomainHandler(w http.ResponseWriter, r *http.Request) {
	var req SyncDomainRequest
	json.NewDecoder(r.Body).Decode(&req)
	if req.Password != secureToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	localTs := getDomainTimestamp()
	if req.Timestamp > localTs {
		os.WriteFile("/etc/caddy/domains.txt", []byte(req.Domains), 0644)
		saveDomainTimestamp(req.Timestamp)
		utils.ExecCmd("systemctl", "reload", "caddy")
	}
	w.WriteHeader(http.StatusOK)
}

func broadcastPeers(peers []string, exclude string) {
	reqBody := SyncPeersRequest{
		Password: secureToken,
		Peers:    peers,
	}
	data, _ := json.Marshal(reqBody)
	
	for _, p := range peers {
		if p == exclude {
			continue
		}
		http.Post(p+"/api/cluster/peers", "application/json", bytes.NewBuffer(data))
	}
}

func BroadcastDomains(domains string, timestamp int64, password string) {
	peers := getLocalPeers()
	reqBody := SyncDomainRequest{
		Password:  password,
		Domains:   domains,
		Timestamp: timestamp,
	}
	data, _ := json.Marshal(reqBody)
	
	for _, p := range peers {
		http.Post(p+"/api/cluster/sync-domain", "application/json", bytes.NewBuffer(data))
	}
}
