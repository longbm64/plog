package frp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/DangLong/na-server-go/pkg/utils"
)

// GithubRelease represents a tag from github API
type GithubRelease struct {
	TagName string `json:"tag_name"`
}

// GetLatestFRPVersion fetches latest tag from github
func GetLatestFRPVersion() string {
	resp, err := http.Get("https://api.github.com/repos/fatedier/frp/releases/latest")
	if err != nil {
		return "0.56.0" // fallback
	}
	defer resp.Body.Close()

	var rel GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "0.56.0"
	}
	return strings.TrimPrefix(rel.TagName, "v")
}

// InstallFRPS downloads and configures FRP
func InstallFRPS(bindPort, vhostPort int, token string) error {
	fmt.Println("\n\033[0;34m>>> [55%] 4. Dang kich hoat lop mang loi (Core Network)...\033[0m")

	fmt.Println("   - [56%] Dang kiem tra tinh tuong thich...")
	ver := GetLatestFRPVersion()
	url := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/v%s/frp_%s_linux_amd64.tar.gz", ver, ver)

	stop := utils.ShowSpinner("Dang thiet lap cac vung mang")
	utils.ExecCmd("bash", "-c", fmt.Sprintf("curl -sLf %s -o /tmp/frp.tar.gz && tar -xzf /tmp/frp.tar.gz -C /tmp", url))
	utils.ExecCmd("bash", "-c", fmt.Sprintf("mv /tmp/frp_%s_linux_amd64/frps /usr/local/bin/frps", ver))
	utils.ExecCmd("chmod", "+x", "/usr/local/bin/frps")
	stop()

	os.MkdirAll("/etc/frp", 0755)

	frpsConfig := fmt.Sprintf(`bindAddr = "0.0.0.0"
bindPort = %d
vhostHTTPPort = %d

auth.method = "token"
auth.token = "%s"

transport.tcpMux = false
transport.maxPoolCount = 200
transport.heartbeatTimeout = 30

custom404Page = "/var/www/html/404.html"

# Dashboard / Admin API (chi truy cap local)
webServer.addr = "127.0.0.1"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "%s"

[[httpPlugins]]
name = "auth"
addr = "nalink.app:80"
path = "/api/frp/auth"
ops = ["Login", "NewWorkConn"]
`, bindPort, vhostPort, token, token)

	fmt.Println("   - [62%] Dang tao cau hinh va copy binary...")
	os.WriteFile("/etc/frp/frps.toml", []byte(frpsConfig), 0644)

	serviceFile := `[Unit]
Description=NALink Tunnel Server
After=network.target

[Service]
ExecStart=/usr/local/bin/frps -c /etc/frp/frps.toml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`
	fmt.Println("   - [68%] Dang tao dich vu he thong core...")
	os.WriteFile("/etc/systemd/system/frps.service", []byte(serviceFile), 0644)

	return nil
}
