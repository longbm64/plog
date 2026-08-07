package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

var secureToken string

type ConnectionInfo struct {
	BindPort  string `json:"bind_port"`
	VhostPort string `json:"vhost_port"`
	Token     string `json:"token"`
}

func getFRPSConfig() *ConnectionInfo {
	content, err := os.ReadFile("/etc/frp/frps.toml")
	if err != nil {
		return nil
	}

	info := &ConnectionInfo{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bindPort") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				info.BindPort = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "vhostHTTPPort") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				info.VhostPort = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "auth.token") {
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				info.Token = strings.Trim(strings.TrimSpace(parts[1]), "\"")
			}
		}
	}
	return info
}

func ConnectionInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	password := r.FormValue("password")
	if password == "" { // support JSON body too
		var req struct {
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		password = req.Password
	}

	if password != secureToken {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Unauthorized"}`))
		return
	}

	info := getFRPSConfig()
	if info == nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "FRPS not configured"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func CheckHandler(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	content, err := os.ReadFile("/etc/caddy/domains.txt")
	if err != nil {
		// If file doesn't exist, fallback to allow all
		w.WriteHeader(http.StatusOK)
		return
	}

	lines := strings.Split(string(content), "\n")
	hasAnyDomain := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			hasAnyDomain = true
			if line == domain {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}

	if !hasAnyDomain {
		// If file is empty, allow all
		w.WriteHeader(http.StatusOK)
		return
	}

	// If file has domains and no match, reject
	w.WriteHeader(http.StatusForbidden)
}

func StartDaemon(port string, secToken string) {
	secureToken = secToken
	http.HandleFunc("/api/connection-info", ConnectionInfoHandler)
	http.HandleFunc("/check", CheckHandler)

	fmt.Printf("Starting NALink Server Daemon on 0.0.0.0:%s\n", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Daemon loi:", err)
	}
}

func InstallDaemonService(apiPort int, secToken string) error {
	binPath, err := os.Executable()
	if err != nil {
		return err
	}

	tokenDir := "/etc/nalink"
	tokenFile := tokenDir + "/.token"
	os.MkdirAll(tokenDir, 0700)
	os.WriteFile(tokenFile, []byte(secToken), 0600)

	conf := fmt.Sprintf(`[Unit]
Description=NALink Server Daemon API
After=network.target

[Service]
ExecStart=%s daemon --port=%d --token-file=/etc/nalink/.token
Restart=always
User=root

[Install]
WantedBy=multi-user.target
`, binPath, apiPort)

	os.WriteFile("/etc/systemd/system/na-server-daemon.service", []byte(conf), 0644)
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "na-server-daemon").Run()
	exec.Command("systemctl", "restart", "na-server-daemon").Run()
	return nil
}
