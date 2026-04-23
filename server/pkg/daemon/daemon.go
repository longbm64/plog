package daemon

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/DangLong/na-server-go/pkg/utils"
)

var domainFile = "/etc/caddy/domains.txt"
var secureToken string
var daemonPort string

// blacklistedPorts chua cac port pho bien can tranh khi cap phat ngau nhien
var blacklistedPorts = map[int]bool{
	10000: true, 11211: true, 27017: true, 27018: true, 33060: true, 50000: true,
}

// domainRegex kiem tra domain hop le (C2 security fix)
var domainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// isValidPort kiem tra port la so nguyen hop le trong khoang 1-65535
func isValidPort(portStr string) bool {
	p, err := strconv.Atoi(portStr)
	return err == nil && p >= 1 && p <= 65535
}

// generateSafePort sinh mot port ngau nhien an toan, kiem tra thuc te khong bi bind
func generateSafePort(exclude string) string {
	for {
		p := rand.Intn(55000) + 10000
		if blacklistedPorts[p] || fmt.Sprintf("%d", p) == exclude {
			continue
		}
		// Kiem tra port nay co dang duoc bind boi tien trinh nao khong
		ssOut, _ := utils.ExecCmd("bash", "-c", fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%d\\b'", p))
		if strings.TrimSpace(ssOut) == "" {
			return fmt.Sprintf("%d", p)
		}
	}
}

func AskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == "GET" {
		domain := r.URL.Query().Get("domain")
		fmt.Printf("[ASK] Kiem tra domain: %s\n", domain)

		if domain == "" {
			fmt.Printf("[ASK] Domain dang trong, tra ve 403\n")
			w.WriteHeader(http.StatusForbidden)
			return
		}

		content := utils.ReadFileContent(domainFile)
		domains := strings.Split(content, "\n")
		fmt.Printf("[ASK] Domains in file: %v\n", domains)

		for _, d := range domains {
			if strings.TrimSpace(d) == domain {
				fmt.Printf("[ASK] Domain found, returning 200 OK\n")
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		fmt.Printf("[ASK] Domain not found, returning 403\n")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if r.Method == "POST" {
		r.ParseForm()
		token := r.FormValue("token")
		domain := r.FormValue("domain")
		port := r.FormValue("port")
		action := r.FormValue("action")

		if token != secureToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if port != "" {
			// BẢO MẬT C2: Validate port
			if !isValidPort(port) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("Invalid port number"))
				return
			}
			if action == "delete_port" {
				utils.ExecCmd("ufw", "delete", "allow", port+"/tcp")
				w.Write([]byte("Port closed successfully"))
			} else {
				// Kiem tra port co dang duoc tien trinh nao THUC SU bind khong (khong chi UFW)
				ssOut, _ := utils.ExecCmd("bash", "-c", fmt.Sprintf("ss -tlnp 2>/dev/null | grep -E ':%s\\b'", port))
				portInUse := strings.TrimSpace(ssOut) != ""

				if portInUse {
					// Port dang bi bind boi tien trinh khac (VD: khach cu cua FRPS tren server moi)
					// Sinh port an toan moi de goi y cho client
					suggestedPort := generateSafePort(port)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict) // 409
					fmt.Fprintf(w, `{"conflict":true,"message":"Port %s dang duoc su dung boi tien trinh khac","suggested_port":"%s"}`, port, suggestedPort)
				} else {
					// Port chua bi bind → mo UFW binh thuong
					utils.ExecCmd("ufw", "allow", port+"/tcp")
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"conflict":false,"message":"Port opened successfully"}`)
				}
			}
			return
		}

		if domain != "" {
			domain = strings.ToLower(strings.TrimSpace(domain))

			// BẢO MẬT C2: Validate domain format
			if !domainRegex.MatchString(domain) || len(domain) > 253 {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("Invalid domain format"))
				return
			}

			if action == "delete" {
				// Xoa domain tu domain.txt
				content := utils.ReadFileContent(domainFile)
				existing := strings.Split(content, "\n")
				var newDomains []string
				for _, d := range existing {
					if strings.TrimSpace(d) != "" && strings.TrimSpace(d) != domain {
						newDomains = append(newDomains, strings.TrimSpace(d))
					}
				}
				os.WriteFile(domainFile, []byte(strings.Join(newDomains, "\n")+"\n"), 0644)

				// Cap nhat Caddy config
				updateCaddyConfig()
				w.Write([]byte("Domain deleted successfully"))
			} else {
				// Kiem tra domain da ton tai chua
				content := utils.ReadFileContent(domainFile)
				existing := strings.Split(content, "\n")
				found := false
				for _, d := range existing {
					if strings.TrimSpace(d) == domain {
						found = true
						break
					}
				}

				if found {
					w.Write([]byte("Domain already exists"))
				} else {
					// Them domain vao domains.txt
					f, _ := os.OpenFile(domainFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
					if f != nil {
						f.WriteString(domain + "\n")
						f.Close()
					}

					// Cap nhat Caddy config
					updateCaddyConfig()
					w.Write([]byte("Domain added successfully"))
				}
			}
			return
		}

		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing domain or port parameter"))
	}
}

// StartDaemon starts the HTTP server acting as caddy ask logic
func StartDaemon(port string, secToken string) {
	secureToken = secToken
	daemonPort = port
	http.HandleFunc("/check", AskHandler) // compatibility if proxy needs /check
	http.HandleFunc("/", AskHandler)
	http.HandleFunc("/system-info", SystemInfoHandler)

	fmt.Printf("Starting NALink Server Daemon on 0.0.0.0:%s\n", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Daemon loi:", err)
	}
}

// InstallDaemonService installs Go binary as system service to handle Caddy Ask API
func InstallDaemonService(apiPort int, secToken string) error {
	binPath, err := os.Executable()
	if err != nil {
		return err
	}

	// BẢO MẬT C3: Lưu token vào file thay vì command-line
	tokenDir := "/etc/nalink"
	tokenFile := tokenDir + "/.token"
	os.MkdirAll(tokenDir, 0700)
	os.WriteFile(tokenFile, []byte(secToken), 0600)

	conf := fmt.Sprintf(`[Unit]
Description=NALink Server Daemon
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

// SystemInfoHandler tra ve thong tin he thong VPS
func SystemInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Removed wildcard CORS origin for security. The daemon API should only be accessed via local reverse proxy.
	// w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Kiêm tra token
	token := r.Header.Get("Authorization")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Remove "Bearer " prefix
	token = strings.TrimPrefix(token, "Bearer ")

	if token != secureToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Lây thông tin he thong
	systemInfo := map[string]interface{}{
		"cpu":      getCPUInfo(),
		"memory":   getMemoryInfo(),
		"swap":     getSwapInfo(),
		"uptime":   getUptime(),
		"load_avg": getLoadAverage(),
		"disk":     getDiskInfo(),
		"network":  getNetworkInfo(),
		"ssh":      getSSHInfo(),
		"os":       getOSInfo(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(systemInfo)
}

func getVhostPort() string {
	content, err := os.ReadFile("/etc/frp/frps.toml")
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "vhostHTTPPort") {
				parts := strings.Split(line, "=")
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return "37213" // fallback
}

// updateCaddyConfig cap nhat Caddyfile voi danh sach domain
func updateCaddyConfig() {
	content := utils.ReadFileContent(domainFile)
	domains := strings.Split(content, "\n")

	apiPort := daemonPort
	if apiPort == "" {
		apiPort = "43781" // fallback
	}

	vPortStr := getVhostPort()

	// Tao Caddy config moi
	var caddyConfig strings.Builder

	caddyConfig.WriteString(fmt.Sprintf(`{
    on_demand_tls {
        ask http://127.0.0.1:%s/check
    }
    servers {
        trusted_proxies static 127.0.0.1/8
    }
}

`, apiPort))

	// Tao config cho tung domain
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		caddyConfig.WriteString(fmt.Sprintf(`%s {
    tls {
        on_demand
    }
    handle {
        reverse_proxy 127.0.0.1:%s
    }
}

`, domain, vPortStr))
	}

	// Neu khong co domain nao, su dung fallback config
	if len(domains) == 0 || (len(domains) == 1 && domains[0] == "") {
		caddyConfig.WriteString(fmt.Sprintf(`:80, :443 {
    tls {
        on_demand
    }
    handle {
        reverse_proxy 127.0.0.1:%s
    }
}
`, vPortStr))
	}

	// Ghi Caddyfile moi
	configContent := caddyConfig.String()
	err := os.WriteFile("/etc/caddy/Caddyfile", []byte(configContent), 0644)
	if err != nil {
		fmt.Printf("[CADDY] Loi ghi Caddyfile: %v\n", err)
		return
	}

	fmt.Printf("[CADDY] Caddyfile moi:\n%s\n", configContent)

	// Reload Caddy
	output, err := utils.ExecCmd("caddy", "reload", "--config", "/etc/caddy/Caddyfile")
	if err != nil {
		fmt.Printf("[CADDY] Loi reload Caddy: %v, output: %s\n", err, output)
	} else {
		fmt.Printf("[CADDY] Reload Caddy thanh cong: %s\n", output)
	}
}

// getCPUInfo lấy thông tin CPU
func getCPUInfo() map[string]interface{} {
	cpuModel := utils.ReadFileContent("/proc/cpuinfo")
	lines := strings.Split(cpuModel, "\n")
	var modelName string
	for _, line := range lines {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				modelName = strings.TrimSpace(parts[1])
				break
			}
		}
	}

	// Đếm số core
	coreCount := runtime.NumCPU()

	return map[string]interface{}{
		"model": modelName,
		"cores": coreCount,
		"usage": getCPUUsage(),
	}
}

// getCPUUsage lấy % CPU usage
func getCPUUsage() float64 {
	// Đọc /proc/stat để tính CPU usage
	stat := utils.ReadFileContent("/proc/stat")
	lines := strings.Split(stat, "\n")
	if len(lines) == 0 {
		return 0
	}

	// Lấy dòng đầu tiên (aggregate CPU)
	cpuLine := lines[0]
	if !strings.HasPrefix(cpuLine, "cpu ") {
		return 0
	}

	parts := strings.Fields(cpuLine)
	if len(parts) < 8 {
		return 0
	}

	// Tính CPU usage từ idle, total
	var idle, total uint64
	for i, part := range parts[1:8] {
		val, _ := strconv.ParseUint(part, 10, 64)
		total += val
		if i == 3 { // idle time
			idle = val
		}
	}

	if total == 0 {
		return 0
	}

	usage := float64(total-idle) / float64(total) * 100
	return usage
}

// getMemoryInfo lấy thông tin RAM
func getMemoryInfo() map[string]interface{} {
	memInfo := utils.ReadFileContent("/proc/meminfo")
	lines := strings.Split(memInfo, "\n")

	var total, available, used uint64
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				total, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				available, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		}
	}

	used = total - available

	return map[string]interface{}{
		"total_mb":      total / 1024 / 1024,
		"used_mb":       used / 1024 / 1024,
		"available_mb":  available / 1024 / 1024,
		"usage_percent": float64(used) / float64(total) * 100,
	}
}

// getSwapInfo lấy thông tin SWAP
func getSwapInfo() map[string]interface{} {
	memInfo := utils.ReadFileContent("/proc/meminfo")
	lines := strings.Split(memInfo, "\n")

	var total, free uint64
	for _, line := range lines {
		if strings.HasPrefix(line, "SwapTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				total, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		} else if strings.HasPrefix(line, "SwapFree:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				free, _ = strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			}
		}
	}

	used := total - free

	return map[string]interface{}{
		"total_mb": total / 1024 / 1024,
		"used_mb":  used / 1024 / 1024,
		"free_mb":  free / 1024 / 1024,
		"usage_percent": func() float64 {
			if total == 0 {
				return 0
			}
			return float64(used) / float64(total) * 100
		}(),
	}
}

// getUptime lấy thời gian uptime
func getUptime() string {
	uptime := utils.ReadFileContent("/proc/uptime")
	if uptime == "" {
		return "Unknown"
	}

	parts := strings.Fields(uptime)
	if len(parts) == 0 {
		return "Unknown"
	}

	seconds, _ := strconv.ParseFloat(parts[0], 64)
	days := int(seconds) / 86400
	hours := int(seconds) % 86400 / 3600
	minutes := int(seconds) % 3600 / 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// getLoadAverage lấy load average
func getLoadAverage() []float64 {
	loadAvg := utils.ReadFileContent("/proc/loadavg")
	if loadAvg == "" {
		return []float64{0, 0, 0}
	}

	parts := strings.Fields(loadAvg)
	if len(parts) < 3 {
		return []float64{0, 0, 0}
	}

	var loads []float64
	for i := 0; i < 3; i++ {
		if val, err := strconv.ParseFloat(parts[i], 64); err == nil {
			loads = append(loads, val)
		} else {
			loads = append(loads, 0)
		}
	}

	return loads
}

// getDiskInfo lấy thông tin disk
func getDiskInfo() map[string]interface{} {
	// Lấy thông tin disk root
	output, _ := utils.ExecCmd("df", "-h", "/")
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return map[string]interface{}{
			"total_gb":      0,
			"used_gb":       0,
			"free_gb":       0,
			"usage_percent": 0,
		}
	}

	// Dòng thứ 2 là thông tin disk
	fields := strings.Fields(lines[1])
	if len(fields) < 6 {
		return map[string]interface{}{
			"total_gb":      0,
			"used_gb":       0,
			"free_gb":       0,
			"usage_percent": 0,
		}
	}

	size := fields[1]
	used := fields[2]
	avail := fields[3]
	usage := fields[4]

	// Chuyển đổi sang GB
	totalGB := parseSizeToGB(size)
	usedGB := parseSizeToGB(used)
	freeGB := parseSizeToGB(avail)
	usagePercent := parseUsagePercent(usage)

	return map[string]interface{}{
		"total_gb":      totalGB,
		"used_gb":       usedGB,
		"free_gb":       freeGB,
		"usage_percent": usagePercent,
	}
}

// parseSizeToGB chuyển đổi size string sang GB
func parseSizeToGB(size string) float64 {
	if len(size) == 0 {
		return 0
	}

	numStr := size[:len(size)-1]
	unit := string(size[len(size)-1])

	num, _ := strconv.ParseFloat(numStr, 64)

	switch unit {
	case "G":
		return num
	case "M":
		return num / 1024
	case "K":
		return num / 1024 / 1024
	default:
		return 0
	}
}

// parseUsagePercent chuyển đổi usage string sang percent
func parseUsagePercent(usage string) float64 {
	if len(usage) == 0 {
		return 0
	}

	percentStr := strings.TrimSuffix(usage, "%")
	percent, _ := strconv.ParseFloat(percentStr, 64)
	return percent
}

// getSSHInfo lấy thông tin SSH port
func getSSHInfo() map[string]interface{} {
	// Lấy SSH port từ /etc/ssh/sshd_config
	sshConfig := utils.ReadFileContent("/etc/ssh/sshd_config")
	lines := strings.Split(sshConfig, "\n")

	var sshPort string = "22" // Default SSH port
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Port ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				sshPort = strings.TrimSpace(parts[1])
				break
			}
		}
	}

	// Kiểm tra SSH service có đang chạy không
	sshStatus := "unknown"
	if output, err := utils.ExecCmd("systemctl", "is-active", "sshd"); err == nil {
		if strings.Contains(output, "active") {
			sshStatus = "running"
		} else if strings.Contains(output, "inactive") {
			sshStatus = "stopped"
		}
	}

	return map[string]interface{}{
		"port":   sshPort,
		"status": sshStatus,
	}
}

// getNetworkInfo lấy thông tin network
func getNetworkInfo() map[string]interface{} {
	// Lấy IP public
	ipOutput, _ := utils.ExecCmd("curl", "-s", "ifconfig.me")
	publicIP := strings.TrimSpace(ipOutput)

	// Lấy interfaces
	interfaces := utils.ReadFileContent("/proc/net/dev")
	lines := strings.Split(interfaces, "\n")

	var activeInterfaces []map[string]interface{}
	for _, line := range lines {
		if strings.Contains(line, ":") && !strings.HasPrefix(line, "Inter-") {
			parts := strings.Fields(line)
			if len(parts) >= 10 {
				iface := strings.TrimSuffix(parts[0], ":")
				rxBytes, _ := strconv.ParseUint(parts[1], 10, 64)
				txBytes, _ := strconv.ParseUint(parts[9], 10, 64)

				activeInterfaces = append(activeInterfaces, map[string]interface{}{
					"name":     iface,
					"rx_bytes": rxBytes,
					"tx_bytes": txBytes,
				})
			}
		}
	}

	return map[string]interface{}{
		"public_ip":  publicIP,
		"interfaces": activeInterfaces,
	}
}

// getOSInfo lấy thông tin hệ điều hành
func getOSInfo() map[string]interface{} {
	osName := "Unknown"
	osVersion := ""

	content := utils.ReadFileContent("/etc/os-release")
	if content != "" {
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(line, "NAME=") {
				osName = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
			}
			if strings.HasPrefix(line, "VERSION_ID=") {
				osVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
			}
		}
	}

	return map[string]interface{}{
		"name":    osName,
		"version": osVersion,
	}
}
