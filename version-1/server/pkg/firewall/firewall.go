package firewall

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DangLong/na-server-go/pkg/utils"
)

// SetupUFW configures standard firewall rules (supports ufw + firewalld)
func SetupUFW(apiPort, sshPort, bindPort, vhostPort int) error {
	fmt.Println("\n\033[1;33m>>> [95%] 7. Dang thiet lap vung bao mat mang (Security Zone)...\033[0m")

	if utils.IsDebian() {
		return setupUFW(apiPort, sshPort, bindPort, vhostPort)
	}
	return setupFirewalld(apiPort, sshPort, bindPort, vhostPort)
}

func setupUFW(apiPort, sshPort, bindPort, vhostPort int) error {
	exec.Command("ufw", "--force", "reset").Run()
	utils.ExecCmd("ufw", "allow", "80/tcp")
	utils.ExecCmd("ufw", "allow", "443/tcp")
	utils.ExecCmd("ufw", "allow", fmt.Sprintf("%d/tcp", apiPort))
	utils.ExecCmd("ufw", "allow", fmt.Sprintf("%d/tcp", sshPort))
	utils.ExecCmd("ufw", "allow", fmt.Sprintf("%d/tcp", bindPort))
	utils.ExecCmd("ufw", "allow", "from", "127.0.0.1", "to", "any", "port", fmt.Sprintf("%d", vhostPort), "proto", "tcp")
	utils.ExecCmd("ufw", "deny", fmt.Sprintf("%d/tcp", vhostPort))
	utils.ExecCmd("ufw", "--force", "enable")
	return nil
}

func setupFirewalld(apiPort, sshPort, bindPort, vhostPort int) error {
	// Ensure firewalld is running
	exec.Command("systemctl", "start", "firewalld").Run()
	exec.Command("systemctl", "enable", "firewalld").Run()

	ports := []int{80, 443, apiPort, sshPort, bindPort}
	for _, p := range ports {
		utils.ExecCmd("firewall-cmd", "--permanent", "--add-port="+fmt.Sprintf("%d/tcp", p))
	}

	// vhostPort only from localhost (reject external) via rich rule
	utils.ExecCmd("firewall-cmd", "--permanent", "--add-rich-rule="+
		fmt.Sprintf("rule family=\"ipv4\" source address=\"127.0.0.1\" port port=\"%d\" protocol=\"tcp\" accept", vhostPort))

	utils.ExecCmd("firewall-cmd", "--reload")
	return nil
}

// OpenPort opens a port on the firewall
func OpenPort(port string) {
	if utils.IsDebian() {
		utils.ExecCmd("ufw", "allow", fmt.Sprintf("%s/tcp", port))
	} else {
		utils.ExecCmd("firewall-cmd", "--permanent", "--add-port="+port+"/tcp")
		utils.ExecCmd("firewall-cmd", "--reload")
	}
	fmt.Printf("\033[0;32m✔ Da mo Port %s\033[0m\n", port)
}

// ClosePort closes a port on the firewall
func ClosePort(port string) {
	if utils.IsDebian() {
		utils.ExecCmd("ufw", "delete", "allow", fmt.Sprintf("%s/tcp", port))
	} else {
		utils.ExecCmd("firewall-cmd", "--permanent", "--remove-port="+port+"/tcp")
		utils.ExecCmd("firewall-cmd", "--reload")
	}
	fmt.Printf("\033[0;32m✔ Da dong Port %s\033[0m\n", port)
}

func getFRPSToken() string {
	content, err := os.ReadFile("/etc/frp/frps.toml")
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "webServer.password") {
				parts := strings.Split(line, "=")
				if len(parts) == 2 {
					return strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			}
		}
	}
	return ""
}

func getFRPSProxies() map[string]string {
	portMap := make(map[string]string)
	token := getFRPSToken()
	if token == "" {
		return portMap
	}

	req, _ := http.NewRequest("GET", "http://127.0.0.1:7500/api/proxy/tcp", nil)
	req.SetBasicAuth("admin", token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		var data struct {
			Proxies []struct {
				Name string `json:"name"`
				Conf struct {
					RemotePort int `json:"remote_port"`
				} `json:"conf"`
			} `json:"proxies"`
		}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			for _, p := range data.Proxies {
				portMap[fmt.Sprintf("%d", p.Conf.RemotePort)] = p.Name
			}
		}
	}
	return portMap
}

// ListPorts lists firewall rules and active services
func ListPorts() {
	fmt.Println("\n\033[0;32mDanh sach Port & Dich vu:\033[0m")

	if utils.IsDebian() {
		listPortsUFW()
	} else {
		listPortsFirewalld()
	}
}

func listPortsUFW() {
	fmt.Printf("%-6s | %-12s | %-10s | %s\n", "[ID]", "PORT", "ACTION", "DICH VU DANG CHAY")
	fmt.Println("-------------------------------------------------------------")

	out, _ := utils.ExecCmd("ufw", "status", "numbered")
	lines := strings.Split(out, "\n")

	frpProxies := getFRPSProxies()

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "(v6)") {
			continue
		}
		// Fix spacing issue in UFW IDs (e.g. "[ 1]" -> "[1]")
		l = strings.Replace(l, "[ ", "[", 1)

		if strings.HasPrefix(l, "[") {
			parts := strings.Fields(l)
			if len(parts) >= 3 {
				id := strings.Trim(parts[0], "[]")
				portProto := parts[1]
				action := parts[2]
				portOnly := strings.Split(portProto, "/")[0]

				svcDetails := "N/A"
				if proxyName, exists := frpProxies[portOnly]; exists {
					svcDetails = "\033[0;35m[FRP] " + proxyName + "\033[0m"
				} else {
					svcOut, _ := utils.ExecCmd("bash", "-c", fmt.Sprintf("ss -tulpn 2>/dev/null | grep -E ':%s\\b' | awk -F'\"' '{print $2}' | head -n1", portOnly))
					svcOut = strings.TrimSpace(svcOut)
					if svcOut == "" {
						svcDetails = "\033[1;33m-- Trong --\033[0m"
					} else {
						svcDetails = "\033[0;36m" + svcOut + "\033[0m"
					}
				}
				fmt.Printf("[%2s]   | %-12s | %-10s | %s\n", id, portProto, action, svcDetails)
			}
		}
	}
}

func listPortsFirewalld() {
	fmt.Printf("%-12s | %s\n", "PORT", "DICH VU DANG CHAY")
	fmt.Println("---------------------------------------------")

	out, _ := utils.ExecCmd("firewall-cmd", "--list-ports")
	ports := strings.Fields(strings.TrimSpace(out))

	frpProxies := getFRPSProxies()

	for _, portProto := range ports {
		portOnly := strings.Split(portProto, "/")[0]
		svcDetails := "N/A"
		if proxyName, exists := frpProxies[portOnly]; exists {
			svcDetails = "\033[0;35m[FRP] " + proxyName + "\033[0m"
		} else {
			svcOut, _ := utils.ExecCmd("bash", "-c", fmt.Sprintf("ss -tulpn 2>/dev/null | grep -E ':%s\\b' | awk -F'\"' '{print $2}' | head -n1", portOnly))
			svcOut = strings.TrimSpace(svcOut)
			if svcOut == "" {
				svcDetails = "\033[1;33m-- Trong --\033[0m"
			} else {
				svcDetails = "\033[0;36m" + svcOut + "\033[0m"
			}
		}
		fmt.Printf("%-12s | %s\n", portProto, svcDetails)
	}
}
