package cli

import (
	"bufio"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/DangLong/na-server-go/pkg/caddy"
	"github.com/DangLong/na-server-go/pkg/firewall"
	"github.com/DangLong/na-server-go/pkg/frp"
	"github.com/DangLong/na-server-go/pkg/security"
	"github.com/DangLong/na-server-go/pkg/system"
	"github.com/DangLong/na-server-go/pkg/utils"
)

var AppVersion = "dev"
var BuildDate = "unknown"

func header() {
	fmt.Print("\033[H\033[2J") // clear screen
	fmt.Println("\033[1;36m")
	fmt.Println(`    _  __ ___    __    _         __   `)
	fmt.Println(`   / |/ // _ |  / /   (_)  ___  / /__ `)
	fmt.Println(`  /    // __ | / /__ / /  / _ \/  '_/ `)
	fmt.Println(` /_/|_//_/ |_|/____//_/  /_//_/_/\_\  `)
	fmt.Printf("\033[1;31m   [ SERVER ]\033[0;90m%27s\033[0m\n", AppVersion+" - "+BuildDate)
	fmt.Println("\033[0m")
}

func runInstallProcess(reader *bufio.Reader, isAuto bool) {
	fmt.Println("\n\033[0;36m--- DANH SACH CAI DAT HE THONG (NALINK SERVER) ---\033[0m")
	fmt.Println("   \033[1;33m1.\033[0m Kiem tra va cap nhat DNS")
	fmt.Println("   \033[1;33m2.\033[0m Cap nhat OS va mo rong System")
	fmt.Println("   \033[1;33m3.\033[0m Kiem tra va tao Swap RAM")
	fmt.Println("   \033[1;33m4.\033[0m Cai dat NALink Tunnel Server")
	fmt.Println("   \033[1;33m5.\033[0m Cai dat NALink Web Server")
	fmt.Println("   \033[1;33m6.\033[0m Tang cuong bao mat he thong")
	fmt.Println("   \033[1;33m7.\033[0m Thiet lap Tuong lua")
	fmt.Println("\033[0;36m--------------------------------------------------\033[0m")

	if !isAuto {
		fmt.Print("   >> Ban co chac chan muon tien hanh cai dat he thong khong? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			return
		}
	}

	// Kiem tra DNS truoc khi cai dat
	fmt.Println("\n\033[0;34m>>> [10%] 1. Kiem tra cau hinh DNS he thong...\033[0m")
	resolvConf := utils.ReadFileContent("/etc/resolv.conf")
	has1111 := strings.Contains(resolvConf, "1.1.1.1")
	has8888 := strings.Contains(resolvConf, "8.8.8.8")

	if has1111 && has8888 {
		fmt.Println("   \033[0;32m✔ DNS da duoc cau hinh tot (1.1.1.1 & 8.8.8.8).\033[0m")
	} else {
		missing := []string{}
		if !has8888 {
			missing = append(missing, "8.8.8.8")
		}
		if !has1111 {
			missing = append(missing, "1.1.1.1")
		}
		fmt.Printf("   \033[1;33m[!] DNS thieu: %s\033[0m\n", strings.Join(missing, ", "))

		dnsAns := "y"
		if !isAuto {
			fmt.Print("   Ban co muon cap nhat DNS thanh 1.1.1.1 va 8.8.8.8 de tiep tuc? (y/N): ")
			ans, _ := reader.ReadString('\n')
			dnsAns = strings.TrimSpace(strings.ToLower(ans))
		} else {
			fmt.Println("   [*] Tu dong cap nhat DNS (Auto Install)...")
		}

		if dnsAns == "y" || dnsAns == "yes" {
			dnsContent := "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"
			os.WriteFile("/etc/resolv.conf", []byte(dnsContent), 0644)
			fmt.Println("   \033[0;32m✔ Da cap nhat DNS thanh cong!\033[0m")
		}
	}

	fmt.Println("\n\033[0;34m>>> [*] Dang lay thong tin khoi tao cac Port dich vu...\033[0m")
	bPort, _ := utils.GetFreePort()
	vPort, _ := utils.GetFreePort()
	apiPort, _ := utils.GetFreePort()
	token, _ := utils.GenerateSecureToken(6)
	secToken, _ := utils.GenerateSecureToken(8)
	system.UpdateSystem()
	system.SetupSwap()
	frp.InstallFRPS(bPort, vPort, token)
	caddy.InstallCaddy(apiPort, vPort, secToken)
	sshPort, _ := system.SecurityHarden()
	firewall.SetupUFW(apiPort, sshPort, bPort, vPort)

	// We cannot call system.SaveInfo here yet because adminUser and adminPass haven't been generated.
	// We will call it after generating the credentials.

	cpuStr, _ := utils.ExecCmd("bash", "-c", "nproc")
	ramStr, _ := utils.ExecCmd("bash", "-c", "free -h | awk '/^Mem:/ {print $2}'")
	swapStr, _ := utils.ExecCmd("bash", "-c", "free -h | awk '/^Swap:/ {print $2}'")

	utils.ExecCmd("systemctl", "daemon-reload")
	utils.ExecCmd("systemctl", "enable", "frps", "caddy", "na-server-daemon")
	utils.ExecCmd("systemctl", "restart", "frps", "caddy", "na-server-daemon")
	utils.ExecCmd("bash", "-c", "systemctl restart sshd || systemctl restart ssh")

	// Auto registration callback
	// Read install token info for later registration (after user creation)
	var installTokenStr, installBaseUrl string
	tokenData, err := os.ReadFile("/etc/.nalink_install_token")
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(tokenData)), "|")
		if len(parts) == 2 {
			installTokenStr = parts[0]
			installBaseUrl = parts[1]
		}
	}

	// AUTO-GENERATE SUDO USER
	fmt.Println("\n\033[1;33m[!] LUU Y QUAN TRONG\033[0m")
	fmt.Println("He thong NALink Server vua KHOA 'root' dang nhap qua SSH vi ly do bao mat.")
	fmt.Println("He thong se tu dong tao tai khoan quan tri vien moi de su dung SSH.")

	// Generate random username: na + 6 digits
	digits := "0123456789"
	userSuffix := make([]byte, 6)
	for i := range userSuffix {
		n, _ := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(digits))))
		userSuffix[i] = digits[n.Int64()]
	}
	adminUser := "na" + string(userSuffix)

	// Generate random password: 8 chars, mixed uppercase + lowercase + digits
	charset := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	passBuf := make([]byte, 8)
	for i := range passBuf {
		n, _ := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(charset))))
		passBuf[i] = charset[n.Int64()]
	}
	adminPass := string(passBuf)

	fmt.Printf("   [*] Tai khoan SSH: \033[1;32m%s\033[0m\n", adminUser)
	fmt.Printf("   [*] Mat khau SSH:  \033[1;32m%s\033[0m\n", adminPass)

	// Create user and add to sudo
	fmt.Println("   [*] Dang tao tai khoan va cap quyen Sudo...")
	utils.ExecCmd("useradd", "-m", "-s", "/bin/bash", adminUser)
	utils.ExecCmd("bash", "-c", fmt.Sprintf("echo '%s:%s' | chpasswd", adminUser, adminPass))
	utils.ExecCmd("usermod", "-aG", "sudo", adminUser)
	utils.ExecCmd("usermod", "-aG", "wheel", adminUser)

	fmt.Printf("   \033[0;32m✔ Da tao tai khoan '%s' voi quyen ROOT (sudo) thanh cong!\033[0m\n", adminUser)

	// Save Info after generating admin credentials
	system.SaveInfo(strings.TrimSpace(cpuStr)+" cores", strings.TrimSpace(ramStr), strings.TrimSpace(swapStr), utils.GetPublicIP(), sshPort, bPort, vPort, apiPort, token, secToken, "/etc/caddy/domains.txt", os.Getenv("HOME")+"/thong_tin_vps.txt", adminUser, adminPass)

	// Registration callback WITH admin credentials (after user creation)
	if installTokenStr != "" && installBaseUrl != "" {
		fmt.Println("\n\033[0;34m>>> [*] Dang dong bo cau hinh len Backend NALink...\033[0m")

		// Detect OS info
		osName, osVersion := "Unknown", ""
		if osRelease := utils.ReadFileContent("/etc/os-release"); osRelease != "" {
			for _, line := range strings.Split(osRelease, "\n") {
				if strings.HasPrefix(line, "NAME=") {
					osName = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
				}
				if strings.HasPrefix(line, "VERSION_ID=") {
					osVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
				}
			}
		}

		payload := map[string]interface{}{
			"token":      installTokenStr,
			"ip":         utils.GetPublicIP(),
			"port":       bPort,
			"frp_token":  token,
			"api_port":   apiPort,
			"api_token":  secToken,
			"admin_user": adminUser,
			"admin_pass": adminPass,
			"os_name":    osName,
			"os_version": osVersion,
		}

		jsonData, _ := json.Marshal(payload)
		resp, reqErr := http.Post(installBaseUrl+"/api/server/register", "application/json", strings.NewReader(string(jsonData)))
		if reqErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				fmt.Println("   \033[0;32m✔ Da dang ky Server vao He thong Quan tri thanh cong!\033[0m")
				os.Remove("/etc/.nalink_install_token")
			} else {
				bodyBytes, _ := io.ReadAll(resp.Body)
				fmt.Printf("   \033[0;31m✖ Khong the dang ky tu dong. %s\033[0m\n", string(bodyBytes))
			}
		} else {
			fmt.Printf("   \033[0;31m✖ Khong the ket noi toi Backend: %v\033[0m\n", reqErr)
		}
	}

	fmt.Println("\n\033[0;32m✅ [100%] Cai dat he thong hoan tat!\033[0m")
	fmt.Println("\n\033[0;36m>>> Tu dong kiem tra toan dien bao mat he thong...\033[0m")
	time.Sleep(1 * time.Second)

	// Auto-run Security Audit (option 7 of security menu)
	security.SecurityAudit(reader)
	fmt.Print("\n(Nhan phim Enter de quay lai Menu chinh...)")
	reader.ReadString('\n')
}

// ShowMenuAuto runs installation in fully unattended mode (called via --auto flag from remote SSH deploy)
func ShowMenuAuto() {
	reader := bufio.NewReader(os.Stdin)

	if _, err := os.Stat("/etc/.nalink_install_token"); err == nil {
		fmt.Println("\n\033[0;32m[+] [AUTO] Che do cai dat tu dong tu xa (Remote Deploy).\033[0m")
		fmt.Println("\033[0;36m    He thong se tu dong cai dat toan bo ha tang NALink Server.\033[0m")
		time.Sleep(1 * time.Second)
		runInstallProcess(reader, true)
		fmt.Println("\n\033[0;32m[+] [AUTO] Cai dat tu dong hoan tat. Thoat chuong trinh.\033[0m")
		os.Exit(0)
	} else {
		fmt.Println("\n\033[0;31m[!] Khong tim thay token cai dat. Khong the chay che do tu dong.\033[0m")
		os.Exit(1)
	}
}

func ShowMenu() {
	reader := bufio.NewReader(os.Stdin)

	// AUTO INSTALL DETECTION
	if _, err := os.Stat("/etc/.nalink_install_token"); err == nil {
		fmt.Println("\n\033[0;32m[+] Phat hien lenh cai dat tu dong tu NALink!\033[0m")
		fmt.Println("\033[0;36m    He thong se tu dong cai dat toan bo ha tang NALink Server.\033[0m")
		fmt.Println("\033[0;34m[>] Bat dau tien trinh cai dat tu dong...\033[0m")
		time.Sleep(1 * time.Second)
		runInstallProcess(reader, true)
		fmt.Println("\033[0;33m[>] Chuyen sang Menu chinh...\033[0m")
		time.Sleep(1 * time.Second)
	}

	for {
		header()

		fmt.Println("   \033[0;33m========== TOOL QUAN LY VPS ==========\033[0m")
		fmt.Println("   1.  Quan ly Domain")
		fmt.Println("   2.  Quan ly Port")
		fmt.Println("   3.  Cap nhat Page 404 (Trang mac dinh)")
		fmt.Println("   4.  Xem trang thai & Restart dich vu")
		fmt.Println("   5.  Cong cu He thong (Set IP tinh, Quan ly User)")
		fmt.Println("   6.  Kiem tra va cap nhat phien ban")
		fmt.Println("   7.  Xem thong tin VPS")
		fmt.Println("   8.  Cap nhat OS & Cai dat he thong NALink")
		fmt.Println("   9.  Chong tan cong & Bao mat nang cao")
		fmt.Println("   10. Go cai dat")
		fmt.Println("   0.  Thoat")
		fmt.Println("")
		fmt.Print("   🔹 Xin moi chon (0-10): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		fmt.Println("")

		skipPause := false

		switch input {
		case "1":
			domainMenu(reader)
			skipPause = true
		case "2":
			portMenu(reader)
			skipPause = true
		case "3":
			pageMenu(reader)
			skipPause = true
		case "4":
			fmt.Println("\n\033[0;36m--- TRANG THAI HE THONG ---\033[0m")
			for _, svc := range []string{"frps", "caddy", "na-server-daemon"} {
				out, _ := utils.ExecCmd("systemctl", "is-active", svc)
				status := strings.TrimSpace(out)
				if status != "active" {
					status = "\033[0;31mDUNG\033[0m"
				} else {
					status = "\033[0;32mHOAT DONG\033[0m"
				}
				fmt.Printf("%-10s : %s\n", svc, status)
			}

			fmt.Print("\n   >>> Ban co muon khoi dong lai tat ca cac dich vu khong? (y/n): ")
			ans, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(ans)) == "y" {
				fmt.Println("   [*] Dang khoi dong lai dich vu...")
				utils.ExecCmd("systemctl", "restart", "frps", "caddy", "na-server-daemon")
				fmt.Println("   \033[0;32m\u2714 Thanh cong!\033[0m")
				skipPause = true
			}
		case "5":
			system.SystemToolsMenu()
			skipPause = true
		case "6":
			checkAndUpdateCLI()
			skipPause = true
		case "7":
			info := utils.ReadFileContent(os.Getenv("HOME") + "/thong_tin_vps.txt")
			if info == "" {
				fmt.Println("\033[0;31mChua co thong tin may chu.\033[0m")
			} else {
				fmt.Println(info)
			}
		case "8":
			runInstallProcess(reader, false)
			skipPause = true
		case "9":
			securityMenu(reader)
			skipPause = true
		case "10":
			fmt.Println("\n   \033[0;31m[!] CANH BAO QUAN TRONG \033[0m")
			fmt.Println("   Viec go cai dat se XOA TOAN BO cau hinh mang cua he thong NALink.")
			fmt.Println("   Tat ca cac Domain va Port dieu huong se bi dong lai.")
			fmt.Print("   >> Ban co chac chan muon tien hanh go cai dat NALink Server? (y/N): ")

			confirm, _ := reader.ReadString('\n')
			confirm = strings.TrimSpace(strings.ToLower(confirm))

			if confirm == "y" || confirm == "yes" {
				fmt.Println("\n   [*] Dang don dep du lieu he thong...")

				utils.ExecCmd("systemctl", "stop", "frps", "caddy", "na-server-daemon")
				utils.ExecCmd("systemctl", "disable", "frps", "caddy", "na-server-daemon")

				os.Remove("/etc/systemd/system/frps.service")
				os.Remove("/etc/systemd/system/na-server-daemon.service")
				utils.ExecCmd("systemctl", "daemon-reload")

				os.RemoveAll("/etc/frp")
				os.RemoveAll("/etc/caddy")
				os.Remove("/usr/local/bin/frps")
				os.Remove("/usr/local/bin/caddy")
				os.Remove(os.Getenv("HOME") + "/thong_tin_vps.txt")

				execPath, _ := os.Executable()
				os.Remove(execPath)
				os.Remove("/usr/local/bin/nalink")
				os.Remove("/usr/local/bin/na")

				fmt.Println("   [OK] Go cai dat thanh cong. Da xoa toan bo he thong.")
				fmt.Println("   Tam biet!")
				os.Exit(0)
			} else {
				fmt.Println("   [*] Da huy go cai dat.")
			}
		case "0":
			fmt.Println("   👋 Tam biet!")
			return
		default:
			fmt.Println("❌ Lua chon khong hop le, vui long thu lai.")
		}

		if !skipPause {
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}

type PassThru struct {
	io.Reader
	total    int64
	length   int64
	progress int
}

func (pt *PassThru) Read(p []byte) (int, error) {
	n, err := pt.Reader.Read(p)
	if n > 0 {
		pt.length += int64(n)
		if pt.total > 0 {
			percent := int(float64(pt.length) / float64(pt.total) * 100)
			if percent > pt.progress {
				pt.progress = percent
				fmt.Printf("\r   [+] Dang tai xuong: %d%%", percent)
			}
		} else {
			fmt.Printf("\r   [+] Dang tai xuong: %d bytes", pt.length)
		}
	}
	return n, err
}

type VersionInfo struct {
	Version string `json:"version"`
	Date    string `json:"date"`
}

func checkAndUpdateCLI() {
	fmt.Println("   [*] Dang kiem tra phien ban moi tren he thong...")

	resp, err := http.Get("https://nalink.app/server-builds/version.json")
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Println("   [!] Khong the kiem tra phien ban. Dang thuc thi tai xuong ban moi nhat hien co...")
		updateCLI()
		return
	}
	defer resp.Body.Close()

	var info VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		updateCLI()
		return
	}

	if info.Version == "" || info.Version == "vdev" || info.Version <= AppVersion {
		fmt.Printf("   [OK] Ban dang su dung phien ban may chu moi nhat (%s).\n", AppVersion)
		fmt.Print("   >>> Ban co muon ep buoc tai lai tu may chu goc khong? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans == "y" || ans == "yes" {
			updateCLI()
		}
		return
	}

	fmt.Printf("   [*] Phat hien phien ban Server moi: \033[1;32m%s\033[0m (Phat hanh: %s)\n", info.Version, info.Date)
	fmt.Printf("   [*] Xem chi tiet tai: https://nalink.app/logs\n")
	fmt.Print("   >>> Ban co muon cap nhat ngay bay gio khong? (y/n): ")

	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))

	if ans == "y" || ans == "yes" {
		updateCLI()
	} else {
		fmt.Println("   [*] Da huy cap nhat.")
	}
}

func updateCLI() {
	fmt.Println("   [*] Dang tai xuong ban cap nhat Server...")

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	osName := "Linux"
	if goos == "darwin" {
		osName = "Darwin"
	}

	archName := goarch
	if goarch == "amd64" {
		archName = "x86_64"
	} else if goarch == "arm" {
		archName = "armv7l"
	}

	fileName := fmt.Sprintf("nalink-%s-%s", osName, archName)
	downloadUrl := fmt.Sprintf("https://nalink.app/server-builds/%s", fileName)

	execPath, err := os.Executable()
	if err != nil {
		fmt.Println("   [!] Khong the xac dinh chuong trinh hien tai.")
		return
	}

	client := http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(downloadUrl)
	if err != nil || resp.StatusCode != 200 {
		fmt.Println("   [!] Loi: Khong the ket noi hoac khong tim thay ban cap nhat tren may chu.")
		return
	}
	defer resp.Body.Close()

	tmpPath := execPath + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Printf("   [!] Loi quyen: Khong the ghi de file (%v)\n", err)
		return
	}

	readerWithProgress := &PassThru{
		Reader: resp.Body,
		total:  resp.ContentLength,
	}

	_, err = io.Copy(tmpFile, readerWithProgress)
	tmpFile.Close()
	fmt.Println()

	if err != nil {
		os.Remove(tmpPath)
		fmt.Println("   [!] Loi tai file: Mang khong on dinh hoac bi ngat ket noi.")
		return
	}

	// Rename the old executable to avoid "text file busy" errors in some linux systems
	os.Rename(execPath, execPath+".old")
	err = os.Rename(tmpPath, execPath)
	if err != nil {
		exec.Command("mv", tmpPath, execPath).Run()
	}

	// Restart daemon if it is running
	exec.Command("systemctl", "restart", "na-server-daemon").Run()
	os.Remove(execPath + ".old")

	fmt.Println("   [OK] Cap nhat thanh cong! Phien ban moi da duoc cai dat (Da Restart Daemon).")
	fmt.Print("   >>> Nhan phim \033[1;36mEnter\033[0m de khoi dong lai Menu...")

	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')

	syscall.Exec(execPath, []string{execPath}, os.Environ())
}

func pageMenu(reader *bufio.Reader) {
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("\033[1;33m              \U0001f4c4 CAP NHAT PAGE 404\033[0m")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("   \033[1;33m1.\033[0m Khoi phuc trang Loi 404 (Trang mac dinh)")
		fmt.Println("   \033[1;33m2.\033[0m Chinh sua trang Loi 404 (nano)")
		fmt.Println("\033[0;36m----------------------------------------------------\033[0m")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai Menu chinh")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Print(" \u2794 Nhap lua chon cua ban: ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Println("   Khoi phuc trang loi 404 mac dinh...")
			caddy.Generate404HTML()
			fmt.Println("   \033[0;32m\u2714 Da khoi phuc trang 404 mac dinh!\033[0m")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "2":
			cmd := exec.Command("nano", "/var/www/html/404.html")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		case "0":
			return
		default:
			fmt.Println("\033[0;31m\u274c Lua chon khong hop le!\033[0m")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}

func domainMenu(reader *bufio.Reader) {
	domainFile := "/etc/caddy/domains.txt"
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("\033[1;33m              \U0001f310 QUAN LY DOMAIN\033[0m")
		fmt.Println("\033[0;36m====================================================\033[0m")

		// Hien thi danh sach domain
		content := utils.ReadFileContent(domainFile)
		var domains []string
		for _, d := range strings.Split(content, "\n") {
			d = strings.TrimSpace(d)
			if d != "" {
				domains = append(domains, d)
			}
		}

		if len(domains) == 0 {
			fmt.Println("   (Chua co domain nao)")
		} else {
			fmt.Println("   Danh sach Domain dang hoat dong:")
			for i, d := range domains {
				fmt.Printf("   %d. %s\n", i+1, d)
			}
		}

		fmt.Println("\033[0;36m----------------------------------------------------\033[0m")
		fmt.Println("   \033[1;33m1.\033[0m Them Domain moi")
		fmt.Println("   \033[1;33m2.\033[0m Xoa Domain")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai Menu chinh")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Print(" \u2794 Chon: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			fmt.Print("   Nhap ten mien (VD: example.com): ")
			domain, _ := reader.ReadString('\n')
			domain = strings.TrimSpace(strings.ToLower(domain))
			if domain == "" {
				continue
			}

			// Kiem tra trung
			for _, d := range domains {
				if d == domain {
					fmt.Printf("   [!] Domain '%s' da ton tai.\n", domain)
					fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
					reader.ReadString('\n')
					continue
				}
			}

			f, err := os.OpenFile(domainFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				fmt.Println("   [!] Loi ghi file:", err)
			} else {
				f.WriteString(domain + "\n")
				f.Close()
				fmt.Printf("   \033[0;32m\u2714 Da them domain '%s'.\033[0m\n", domain)
			}
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "2":
			if len(domains) == 0 {
				fmt.Println("   [!] Khong co domain nao de xoa.")
				fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
				reader.ReadString('\n')
				continue
			}
			fmt.Print("   Nhap so thu tu domain muon xoa: ")
			idxStr, _ := reader.ReadString('\n')
			idx := 0
			fmt.Sscanf(strings.TrimSpace(idxStr), "%d", &idx)
			if idx < 1 || idx > len(domains) {
				fmt.Println("   [!] So thu tu khong hop le.")
				fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
				reader.ReadString('\n')
				continue
			}

			removed := domains[idx-1]
			fmt.Printf("   [?] Xoa domain '%s'? (y/N): ", removed)
			confirm, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(confirm)) == "y" {
				var newDomains []string
				for _, d := range domains {
					if d != removed {
						newDomains = append(newDomains, d)
					}
				}
				os.WriteFile(domainFile, []byte(strings.Join(newDomains, "\n")+"\n"), 0644)
				fmt.Printf("   \033[0;32m\u2714 Da xoa domain '%s'.\033[0m\n", removed)
			} else {
				fmt.Println("   [*] Da huy thao tac.")
			}
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "0":
			return
		default:
			fmt.Println("   [!] Lua chon khong hop le.")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}

func portMenu(reader *bufio.Reader) {
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("\033[1;33m              \U0001f6e1 QUAN LY PORT\033[0m")
		fmt.Println("\033[0;36m====================================================\033[0m")

		firewall.ListPorts()

		fmt.Println("\033[0;36m----------------------------------------------------\033[0m")
		fmt.Println("   \033[1;33m1.\033[0m Mo Port moi")
		fmt.Println("   \033[1;33m2.\033[0m Dong Port")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai Menu chinh")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Print(" \u2794 Chon: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			fmt.Print("   Nhap Port muon mo (VD: 8080): ")
			port, _ := reader.ReadString('\n')
			port = strings.TrimSpace(port)
			if port == "" {
				continue
			}
			firewall.OpenPort(port)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "2":
			fmt.Print("   Nhap Port muon dong (VD: 8080): ")
			port, _ := reader.ReadString('\n')
			port = strings.TrimSpace(port)
			if port == "" {
				continue
			}
			firewall.ClosePort(port)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "0":
			return
		default:
			fmt.Println("   [!] Lua chon khong hop le.")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}

func securityMenu(reader *bufio.Reader) {
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("\033[1;33m          \U0001f6e1 CHONG TAN CONG & BAO MAT\033[0m")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("   \033[1;33m1.\033[0m Bao ve he thong chong tan cong mang")
		fmt.Println("   \033[1;33m2.\033[0m Gioi han ket noi chong DDoS")
		fmt.Println("   \033[1;33m3.\033[0m Bao mat nang cao chong truy cap trai phep")
		fmt.Println("   \033[1;33m4.\033[0m Kiem tra trang thai bao mat")
		fmt.Println("   \033[1;33m5.\033[0m Xem danh sach IP dang bi chan")
		fmt.Println("   \033[1;33m6.\033[0m Go chan mot IP")
		fmt.Println("   \033[1;33m7.\033[0m Kiem tra toan dien bao mat he thong")
		fmt.Println("\033[0;36m----------------------------------------------------\033[0m")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai Menu chinh")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Print(" \u2794 Nhap lua chon cua ban: ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			security.SetupSysctlHardening()
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "2":
			security.SetupConnectionLimits()
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "3":
			security.SetupFail2BanAdvanced(reader)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "4":
			security.ShowFail2BanStatus()
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "5":
			security.ShowBlockedIPs()
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "6":
			security.UnbanIP(reader)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "7":
			security.SecurityAudit(reader)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		case "0":
			return
		default:
			fmt.Println("\033[0;31m\u274c Lua chon khong hop le!\033[0m")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}
