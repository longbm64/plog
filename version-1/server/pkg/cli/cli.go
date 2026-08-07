package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/DangLong/na-server-go/pkg/caddy"
	"github.com/DangLong/na-server-go/pkg/frp"
	"github.com/DangLong/na-server-go/pkg/utils"
)

var AppVersion = "v1.0.0-HA"
var BuildDate = "2026-08-07"

func getFRPSStatus() string {
	out, _ := utils.ExecCmd("systemctl", "is-active", "frps")
	status := strings.TrimSpace(out)
	
	if status == "active" {
		return "\033[0;32mĐANG CHẠY\033[0m"
	} else if status == "failed" {
		return "\033[0;31mLỖI (FAILED)\033[0m"
	}
	return "\033[0;31mĐANG DỪNG\033[0m"
}

func getCaddyStatus() string {
	out, _ := utils.ExecCmd("systemctl", "is-active", "caddy")
	status := strings.TrimSpace(out)
	
	if status == "active" {
		return "\033[0;32mĐANG CHẠY\033[0m"
	} else if status == "failed" {
		return "\033[0;31mLỖI (FAILED)\033[0m"
	}
	return "\033[0;31mĐANG DỪNG\033[0m"
}

func getDaemonStatus() string {
	out, _ := utils.ExecCmd("systemctl", "is-active", "na-server-daemon")
	status := strings.TrimSpace(out)
	
	if status == "active" {
		return "\033[0;32mĐANG CHẠY\033[0m"
	} else if status == "failed" {
		return "\033[0;31mLỖI (FAILED)\033[0m"
	}
	return "\033[0;31mĐANG DỪNG\033[0m"
}

func header() {
	fmt.Print("\033[H\033[2J") // clear screen
	fmt.Println("\033[1;36m")
	fmt.Println(`    _  __ ___    __    _         __   `)
	fmt.Println(`   / |/ // _ |  / /   (_)  ___  / /__ `)
	fmt.Println(`  /    // __ | / /__ / /  / _ \/  '_/ `)
	fmt.Println(` /_/|_//_/ |_|/____//_/  /_//_/_/\_\  `)
	fmt.Printf("\033[1;31m   [ SERVER - LITE ]\033[0;90m%27s\033[0m\n", AppVersion+" - "+BuildDate)
	fmt.Println("\033[0m")
	fmt.Printf("   Trạng thái FRPS: %s\n", getFRPSStatus())
	fmt.Printf("   Trạng thái Caddy: %s\n", getCaddyStatus())
	fmt.Printf("   Trạng thái API: %s\n\n", getDaemonStatus())
}

func runInstallFRPS(reader *bufio.Reader) {
	fmt.Println("\n\033[0;36m--- CAI DAT FRPS, CADDY & API DAEMON ---\033[0m")
	
	bPortDef, _ := utils.GetFreePort()
	vPortDef, _ := utils.GetFreePort()
	apiPortDef, _ := utils.GetFreePort()

	fmt.Printf("   Nhap FRP Bind Port (VD: 7000) [%d]: ", bPortDef)
	bPort, _ := reader.ReadString('\n')
	bPort = strings.TrimSpace(bPort)
	if bPort == "" {
		bPort = strconv.Itoa(bPortDef)
	}

	fmt.Printf("   Nhap FRP Vhost HTTP Port (VD: 8080) [%d]: ", vPortDef)
	vPort, _ := reader.ReadString('\n')
	vPort = strings.TrimSpace(vPort)
	if vPort == "" {
		vPort = strconv.Itoa(vPortDef)
	}

	fmt.Print("   Nhap FRP Auth Token [tu dong tao]: ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)
	if token == "" {
		token, _ = utils.GenerateSecureToken(24)
		fmt.Printf("   \033[0;33m[*] Tu dong tao Token: %s\033[0m\n", token)
	}

	fmt.Printf("   Nhap Port cho API Daemon (VD: 7400) [%d]: ", apiPortDef)
	apiPortStr, _ := reader.ReadString('\n')
	apiPortStr = strings.TrimSpace(apiPortStr)
	if apiPortStr == "" {
		apiPortStr = strconv.Itoa(apiPortDef)
	}

	fmt.Print("   Nhap Mat khau de Client ket noi API [tu dong tao]: ")
	apiPass, _ := reader.ReadString('\n')
	apiPass = strings.TrimSpace(apiPass)
	if apiPass == "" {
		apiPass, _ = utils.GenerateSecureToken(32)
		fmt.Printf("   \033[0;33m[*] Tu dong tao Mat khau: %s\033[0m\n", apiPass)
	}

	bPortInt, _ := strconv.Atoi(bPort)
	vPortInt, _ := strconv.Atoi(vPort)
	frp.InstallFRPS(bPortInt, vPortInt, token)

	apiPortInt, _ := strconv.Atoi(apiPortStr)
	caddy.InstallCaddy(apiPortInt, vPortInt, apiPass)

	// Save port to file so we can view it later
	os.MkdirAll("/etc/nalink", 0700)
	os.WriteFile("/etc/nalink/api_port", []byte(apiPortStr), 0644)

	utils.ExecCmd("systemctl", "daemon-reload")
	utils.ExecCmd("systemctl", "enable", "frps")
	utils.ExecCmd("systemctl", "restart", "frps")

	fmt.Println("\n\033[0;32m✅ CAI DAT FRPS HOAN TAT!\033[0m")
	fmt.Printf("   - Bind Port: \033[1;32m%s\033[0m\n", bPort)
	fmt.Printf("   - Vhost Port: \033[1;32m%s\033[0m\n", vPort)
	fmt.Printf("   - Token FRPS: \033[1;32m%s\033[0m\n", token)
	fmt.Println("\n\033[0;36m[THONG TIN API DAEMON CHO CLIENT]\033[0m")
	fmt.Printf("   - API Port: \033[1;32m%s\033[0m\n", apiPortStr)
	fmt.Printf("   - API Pass: \033[1;32m%s\033[0m\n", apiPass)
}

func showConnectionInfo() {
	fmt.Println("\n\033[0;36m--- THONG TIN KET NOI TREN SERVER ---\033[0m")
	
	frpsConf, err := os.ReadFile("/etc/frp/frps.toml")
	if err != nil {
		fmt.Println("   \033[0;31mFRPS chua duoc cai dat.\033[0m")
	} else {
		fmt.Println("\n   \033[1;33m[1] Cau hinh FRPS:\033[0m")
		lines := strings.Split(string(frpsConf), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("       %s\n", line)
			}
		}
	}

	apiPort, err1 := os.ReadFile("/etc/nalink/api_port")
	apiPass, err2 := os.ReadFile("/etc/nalink/.token")

	if err1 == nil && err2 == nil {
		fmt.Println("\n   \033[1;33m[2] Cau hinh API Daemon cho Client:\033[0m")
		fmt.Printf("       - API Port: \033[1;32m%s\033[0m\n", strings.TrimSpace(string(apiPort)))
		fmt.Printf("       - Mật khẩu: \033[1;32m%s\033[0m\n", strings.TrimSpace(string(apiPass)))
		fmt.Println("\n   \033[0;36m* Client co the POST toi http://<ip>:<port>/api/connection-info voi body { \"password\": \"...\" }\033[0m")
	}
}

func manageDomains(reader *bufio.Reader) {
	for {
		fmt.Println("\n\033[0;36m--- QUAN LY DOMAIN (WHITELIST CHO CADDY) ---\033[0m")
		fmt.Println("   \033[0;90m(Neu danh sach trong, he thong se cho phep TAT CA domain)\033[0m")
		
		content, _ := os.ReadFile("/etc/caddy/domains.txt")
		domains := []string{}
		for _, line := range strings.Split(string(content), "\n") {
			if strings.TrimSpace(line) != "" {
				domains = append(domains, strings.TrimSpace(line))
			}
		}

		if len(domains) == 0 {
			fmt.Println("   \033[0;33m[Danh sach hien tai dang TRONG]\033[0m")
		} else {
			fmt.Println("   \033[1;32m[Danh sach cac Domain duoc phep]\033[0m")
			for i, d := range domains {
				fmt.Printf("   %d. %s\n", i+1, d)
			}
		}

		fmt.Println("\n   1. Them Domain moi")
		fmt.Println("   2. Xoa Domain")
		fmt.Println("   3. Xoa TAT CA (Cho phep moi domain)")
		fmt.Println("   0. Quay lai")
		fmt.Print("\n   🔹 Xin moi chon (0-3): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Print("   Nhập Domain cần thêm (VD: a.test.nalink.app): ")
			newDomain, _ := reader.ReadString('\n')
			newDomain = strings.TrimSpace(newDomain)
			if newDomain != "" {
				domains = append(domains, newDomain)
				os.WriteFile("/etc/caddy/domains.txt", []byte(strings.Join(domains, "\n")+"\n"), 0644)
				fmt.Println("   \033[0;32m✔ Đã thêm thành công!\033[0m")
			}
		case "2":
			fmt.Print("   Nhập số thứ tự Domain cần xóa: ")
			numStr, _ := reader.ReadString('\n')
			num, err := strconv.Atoi(strings.TrimSpace(numStr))
			if err == nil && num > 0 && num <= len(domains) {
				domains = append(domains[:num-1], domains[num:]...)
				os.WriteFile("/etc/caddy/domains.txt", []byte(strings.Join(domains, "\n")+"\n"), 0644)
				fmt.Println("   \033[0;32m✔ Đã xóa thành công!\033[0m")
			} else {
				fmt.Println("   \033[0;31m❌ Số thứ tự không hợp lệ!\033[0m")
			}
		case "3":
			os.WriteFile("/etc/caddy/domains.txt", []byte(""), 0644)
			fmt.Println("   \033[0;32m✔ Đã xóa toàn bộ danh sách!\033[0m")
		case "0", "":
			return
		}
	}
}

func restartServices(reader *bufio.Reader) {
	for {
		fmt.Println("\n\033[0;36m--- KHOI DONG LAI DICH VU ---\033[0m")
		fmt.Println("   1. Khoi dong lai TAT CA")
		fmt.Println("   2. Khoi dong lai FRPS")
		fmt.Println("   3. Khoi dong lai Caddy")
		fmt.Println("   4. Khoi dong lai API Daemon")
		fmt.Println("   0. Quay lai")
		fmt.Print("\n   🔹 Xin moi chon (0-4): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Println("   \033[0;33m>>> Dang khoi dong lai tat ca cac dich vu...\033[0m")
			utils.ExecCmd("systemctl", "restart", "frps")
			utils.ExecCmd("systemctl", "restart", "caddy")
			utils.ExecCmd("systemctl", "restart", "na-server-daemon")
			fmt.Println("   \033[0;32m✔ Da khoi dong lai hoan tat!\033[0m")
		case "2":
			fmt.Println("   \033[0;33m>>> Dang khoi dong lai FRPS...\033[0m")
			utils.ExecCmd("systemctl", "restart", "frps")
			fmt.Println("   \033[0;32m✔ Hoan tat!\033[0m")
		case "3":
			fmt.Println("   \033[0;33m>>> Dang khoi dong lai Caddy...\033[0m")
			utils.ExecCmd("systemctl", "restart", "caddy")
			fmt.Println("   \033[0;32m✔ Hoan tat!\033[0m")
		case "4":
			fmt.Println("   \033[0;33m>>> Dang khoi dong lai API Daemon...\033[0m")
			utils.ExecCmd("systemctl", "restart", "na-server-daemon")
			fmt.Println("   \033[0;32m✔ Hoan tat!\033[0m")
		case "0", "":
			return
		default:
			fmt.Println("   \033[0;31m❌ Lua chon khong hop le!\033[0m")
		}
	}
}

func ShowMenuAuto() {
	// Auto mode if needed
}

func ShowMenu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		header()

		fmt.Println("   \033[0;33m========== MENU CAI DAT SERVER ==========\033[0m")
		fmt.Println("   1. Cai dat FRPS, Caddy & API Daemon")
		fmt.Println("   2. Xem thong tin ket noi")
		fmt.Println("   3. Quan ly Domain (Bao mat HTTPS)")
		fmt.Println("   4. Khoi dong lai cac dich vu")
		fmt.Println("   0. Thoat")
		fmt.Println("")
		fmt.Print("   🔹 Xin moi chon (0-4): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			runInstallFRPS(reader)
		case "2":
			showConnectionInfo()
		case "3":
			manageDomains(reader)
		case "4":
			restartServices(reader)
		case "0":
			fmt.Println("   👋 Tam biet!")
			return
		case "":
			continue
		default:
			fmt.Println("❌ Lua chon khong hop le, vui long thu lai.")
		}

		fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
		reader.ReadString('\n')
	}
}
