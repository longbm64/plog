package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/DangLong/na-server-go/pkg/utils"
)

// UpdateSystem runs system update and installs required packages (supports Debian + RHEL)
func UpdateSystem() error {
	fmt.Println("\n\033[1;33m>>> [20%] 2. Dang cap nhat OS (Qua trinh nay co the mat 2-5 phut)...\033[0m")

	if utils.IsDebian() {
		return updateDebian()
	}
	return updateRHEL()
}

func updateDebian() error {
	fmt.Println("   - [20%] Dang don dep cac thiet lap mac dinh...")
	utils.ExecCmd("bash", "-c", "killall apt apt-get dpkg 2>/dev/null || true")
	utils.ExecCmd("bash", "-c", "rm -f /var/lib/apt/lists/lock /var/cache/apt/archives/lock /var/lib/dpkg/lock*")
	utils.ExecCmd("dpkg", "--configure", "-a")

	fmt.Println("   - [21%] Dang toi uu hoa cau truc nhan he thong...")
	optimizeMirror()

	stop := utils.ShowSpinner("Dang dong bo hoa co so du lieu bao mat")
	cmd := exec.Command("apt", "update", "-y")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.CombinedOutput()
	stop()

	stop = utils.ShowSpinner("Dang nang cap cac thanh phan loai")
	cmd = exec.Command("apt", "upgrade", "-y",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "Dpkg::Options::=--force-confdef")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a")
	cmd.CombinedOutput()
	stop()

	stop = utils.ShowSpinner("Dang kich hoat cac lop bao mat mo rong")
	pkgs := []string{"curl", "jq", "python3", "ufw", "fail2ban", "unzip", "zip"}
	args := append([]string{"install", "-y",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "Dpkg::Options::=--force-confdef",
	}, pkgs...)
	cmd = exec.Command("apt", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a")
	cmd.CombinedOutput()
	stop()
	return nil
}

func updateRHEL() error {
	pkgMgr := utils.GetPkgManager()

	stop := utils.ShowSpinner("Dang dong bo hoa co so du lieu bao mat")
	utils.ExecCmd(pkgMgr, "makecache", "-y")
	stop()

	stop = utils.ShowSpinner("Dang nang cap cac thanh phan loai")
	utils.ExecCmd(pkgMgr, "update", "-y")
	stop()

	// EPEL repo (required for fail2ban on CentOS/Rocky)
	stop = utils.ShowSpinner("Dang toi uu hoa cau truc nhan he thong")
	utils.ExecCmd(pkgMgr, "install", "-y", "epel-release")
	stop()

	stop = utils.ShowSpinner("Dang kich hoat cac lop bao mat mo rong")
	pkgs := []string{"curl", "jq", "python3", "firewalld", "fail2ban", "unzip", "zip"}
	args := append([]string{"install", "-y"}, pkgs...)
	cmd := exec.Command(pkgMgr, args...)
	cmd.CombinedOutput()
	stop()

	// Enable and start firewalld
	exec.Command("systemctl", "enable", "firewalld").Run()
	exec.Command("systemctl", "start", "firewalld").Run()

	return nil
}

// optimizeMirror tim va chuyen sang mirror Ubuntu nhanh nhat cho VPS
func optimizeMirror() {
	// Chi ap dung cho Ubuntu
	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains(strings.ToLower(string(osRelease)), "ubuntu") {
		fmt.Println("     Khong phai Ubuntu, bo qua toi uu mirror.")
		return
	}

	// Lay codename (vd: noble, jammy, focal)
	codename := ""
	lines := strings.Split(string(osRelease), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "VERSION_CODENAME=") {
			codename = strings.TrimPrefix(l, "VERSION_CODENAME=")
			codename = strings.TrimSpace(codename)
			break
		}
	}
	if codename == "" {
		return
	}

	// Danh sach mirror chinh thuc (Ubuntu Launchpad verified)
	mirrors := []string{
		// Vietnam (Launchpad verified)
		"http://mirror.bizflycloud.vn/ubuntu",  // BizFly Cloud
		"http://mirror.viettelcloud.vn/ubuntu", // Viettel Cloud
		"http://mirror.vietnix.vn/ubuntu",      // Vietnix
		"http://mirror.azvps.vn/ubuntu",        // AZVPS
		"http://mirror.clearsky.vn/ubuntu",     // ClearSky
		"http://mirrors.bkns.vn/ubuntu",        // BKNS
		"http://mirrors.gofiber.vn/ubuntu",     // Gofiber
		// Singapore
		"http://mirror.sg.gs/ubuntu",               // SG.GS
		"http://sg-mirrors.vhost.vn/ubuntu",        // vHost SG
		"http://download.nus.edu.sg/mirror/ubuntu", // NUS
		// Japan / Korea
		"http://ftp.jaist.ac.jp/pub/Linux/ubuntu", // JAIST
		// Europe
		"http://de.archive.ubuntu.com/ubuntu", // Germany
		"http://fr.archive.ubuntu.com/ubuntu", // France
		// Fallback
		"http://archive.ubuntu.com/ubuntu", // Default
	}

	bestMirror := ""
	bestTime := int64(9999)

	for _, m := range mirrors {
		// Kiem tra HTTP 200 VA do thoi gian phan hoi
		out, _ := utils.ExecCmd("bash", "-c", fmt.Sprintf(
			`timeout 3 curl -so /dev/null -w '%%{http_code} %%{time_total}' '%s/dists/%s/Release' 2>/dev/null || echo "000 9999"`,
			m, codename))
		out = strings.TrimSpace(out)
		out = strings.Trim(out, "'\"")
		fields := strings.Fields(out)
		if len(fields) < 2 {
			continue
		}
		httpCode := fields[0]
		timeStr := fields[1]

		// Chi chap nhan mirror tra ve HTTP 200
		if httpCode != "200" {
			continue
		}

		// Chuyen time_total ve millisecond
		parts := strings.Split(timeStr, ".")
		ms := int64(0)
		if len(parts) == 2 {
			sec, _ := strconv.ParseInt(parts[0], 10, 64)
			frac := parts[1]
			if len(frac) > 3 {
				frac = frac[:3]
			}
			for len(frac) < 3 {
				frac += "0"
			}
			fracMs, _ := strconv.ParseInt(frac, 10, 64)
			ms = sec*1000 + fracMs
		}
		if ms > 0 && ms < bestTime {
			bestTime = ms
			bestMirror = m
		}
	}

	if bestMirror == "" || bestMirror == "http://archive.ubuntu.com/ubuntu" {
		fmt.Println("     Mirror mac dinh da la nhanh nhat, khong can thay doi.")
		return
	}

	fmt.Printf("     Tim thay mirror nhanh nhat: %s (%dms)\n", bestMirror, bestTime)

	// Backup sources.list cu
	utils.ExecCmd("cp", "/etc/apt/sources.list", "/etc/apt/sources.list.bak")

	// Ubuntu 24.04+ dung file DEB822 tai /etc/apt/sources.list.d/ubuntu.sources
	// Phai vo hieu hoa file nay de tranh APT gop nguon cu (de.archive) vao
	deb822 := "/etc/apt/sources.list.d/ubuntu.sources"
	if _, err := os.Stat(deb822); err == nil {
		utils.ExecCmd("cp", deb822, deb822+".bak")
		os.Remove(deb822)
		fmt.Println("     Da vo hieu hoa ubuntu.sources (DEB822) de tranh trung nguon.")
	}

	// Tao sources.list moi
	newSources := fmt.Sprintf("# NALink Auto-Optimized Mirror\n"+
		"deb %s %s main restricted universe multiverse\n"+
		"deb %s %s-updates main restricted universe multiverse\n"+
		"deb %s %s-security main restricted universe multiverse\n"+
		"deb %s %s-backports main restricted universe multiverse\n",
		bestMirror, codename,
		bestMirror, codename,
		bestMirror, codename,
		bestMirror, codename)

	os.WriteFile("/etc/apt/sources.list", []byte(newSources), 0644)
	fmt.Printf("     Da chuyen sang mirror moi thanh cong.\n")
}

// SetupSwap configures swap memory
func SetupSwap() {
	fmt.Println("\n\033[1;33m>>> [40%] 3. Dang kiem tra va thiet lap Swap RAM...\033[0m")

	out, _ := utils.ExecCmd("free", "-m")
	lines := strings.Split(out, "\n")
	var ramMB, swapMB int
	for _, l := range lines {
		if strings.HasPrefix(l, "Mem:") {
			fields := strings.Fields(l)
			if len(fields) > 1 {
				ramMB, _ = strconv.Atoi(fields[1])
			}
		}
		if strings.HasPrefix(l, "Swap:") {
			fields := strings.Fields(l)
			if len(fields) > 1 {
				swapMB, _ = strconv.Atoi(fields[1])
			}
		}
	}

	if swapMB == 0 {
		swapSize := 4096
		if ramMB <= 2500 {
			swapSize = 2048
		}

		fmt.Printf("   - [45%%] Tien hanh pre-allocate tao file giang Swap %dMB...\n", swapSize)
		utils.ExecCmd("fallocate", "-l", fmt.Sprintf("%dM", swapSize), "/swapfile")
		utils.ExecCmd("chmod", "600", "/swapfile")

		fmt.Printf("   - [48%%] Dang dinh dang & kich hoat /swapfile...\n")
		utils.ExecCmd("mkswap", "/swapfile")
		utils.ExecCmd("swapon", "/swapfile")

		fstab, _ := os.ReadFile("/etc/fstab")
		if !strings.Contains(string(fstab), "/swapfile none swap sw 0 0") {
			f, _ := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString("/swapfile none swap sw 0 0\n")
				f.Close()
			}
		}
		fmt.Printf("\033[0;32m✔ Da tao Swap %dMB\033[0m\n", swapSize)
	} else {
		fmt.Printf("\033[0;32m✔ Da co Swap (%dMB)\033[0m\n", swapMB)
	}
}

// SecurityHarden changes SSH port and configures fail2ban
func SecurityHarden() (int, error) {
	fmt.Println("\n\033[1;33m>>> [85%] 6. Dang thiet lap vung bao mat he thong...\033[0m")

	fmt.Println("   - [87%] Dang tao port truy cap tu xa ngau nhien an toan...")
	// Lay SSH port ngau nhien thay vi chon 1 port
	newSSHPort, err := utils.GetFreePort()
	if err != nil {
		newSSHPort = 2244
	}

	// Doi port SSH
	fmt.Printf("   - [90%%] Dang thay doi port truy cap tu xa sang %d va cau hinh bao mat...\n", newSSHPort)
	utils.ExecCmd("sed", "-i", fmt.Sprintf("s/^#\\?Port .*/Port %d/", newSSHPort), "/etc/ssh/sshd_config")

	configs := map[string]string{
		"PermitRootLogin":      "no",
		"PermitEmptyPasswords": "no",
		"MaxAuthTries":         "3",
		"ClientAliveInterval":  "300",
		"ClientAliveCountMax":  "2",
		"X11Forwarding":        "no",
		"UseDNS":               "no",
		"LoginGraceTime":       "60",
	}

	fmt.Println("\033[1;33mDang thiet lap thong so bao mat an toan...\033[0m")

	for k, v := range configs {
		val := fmt.Sprintf("%s %s", k, v)
		checkCmd := fmt.Sprintf("grep -qE '^[#]*[[:space:]]*%s' /etc/ssh/sshd_config", k)
		if err := exec.Command("bash", "-c", checkCmd).Run(); err == nil {
			utils.ExecCmd("sed", "-i", "-E", fmt.Sprintf("s/^[#]*[[:space:]]*%s.*/%s/", k, val), "/etc/ssh/sshd_config")
		} else {
			f, _ := os.OpenFile("/etc/ssh/sshd_config", os.O_APPEND|os.O_WRONLY, 0644)
			if f != nil {
				f.WriteString(val + "\n")
				f.Close()
			}
		}
	}

	fmt.Println("   - [93%] Dang cau hinh bao mat chong truy cap trai phep...")
	// Setup Fail2Ban (log path differs between Debian and RHEL)
	logPath := utils.GetLogPath()
	f2bConf := fmt.Sprintf(`[sshd]
enabled = true
port = %d
filter = sshd
logpath = %s
maxretry = 4
bantime = 3600
findtime = 600
ignoreip = 127.0.0.1/8 ::1
`, newSSHPort, logPath)

	os.WriteFile("/etc/fail2ban/jail.local", []byte(f2bConf), 0644)
	utils.ExecCmd("systemctl", "restart", "fail2ban")

	return newSSHPort, nil
}

// SaveInfo stores install info to file
func SaveInfo(cpu, ram, swap, ip string, sshPort, bPort, vPort, apiPort int, token, secToken, domainFile, infoFile, adminUser, adminPass string) {
	content := fmt.Sprintf(`=============================
    THONG TIN VPS - FRP + Caddy
=============================
CPU Cores       : %s
RAM             : %s
Swap RAM        : %s
IP VPS          : %s
SSH Port        : %d
FRP Bind Port   : %d
FRP Token       : %s
VHOST Port      : %d
API Port        : %d
API Secure Token: %s
Domain File     : %s
=============================
CHUOI CAU HINH  : %s|%d|%s|%d|%s|%s|%s
=============================
[API DOMAIN] LENH THEM TU CLIENT:
curl -X POST -d "token=%s&domain=yourdomain.com" http://%s:%d
[API DOMAIN] LENH XOA TU CLIENT:
curl -X POST -d "token=%s&domain=yourdomain.com&action=delete" http://%s:%d
[API PORT UFW] LENH MO CONG TU CLIENT:
curl -X POST -d "token=%s&port=6000&action=add_port" http://%s:%d
[API PORT UFW] LENH DONG CONG TU CLIENT:
curl -X POST -d "token=%s&port=6000&action=delete_port" http://%s:%d
=============================
`, cpu, ram, swap, ip, sshPort, bPort, token, vPort, apiPort, secToken, domainFile,
		ip, bPort, token, apiPort, secToken, adminUser, adminPass,
		secToken, ip, apiPort, secToken, ip, apiPort, secToken, ip, apiPort, secToken, ip, apiPort)

	os.WriteFile(infoFile, []byte(content), 0644)
}

// SystemToolsMenu displays the OS specific tools loop
func SystemToolsMenu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("\033[H\033[2J") // clear screen
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("\033[1;33m                 🛠 CONG CU HE THONG\033[0m")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Println("   \033[1;33m1.\033[0m Cai dat IP Tinh cho he thong")
		fmt.Println("   \033[1;33m2.\033[0m Quan ly User may (Them/Xoa/Sudo/Password)")
		fmt.Println("\033[0;36m----------------------------------------------------\033[0m")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai Menu chinh")
		fmt.Println("\033[0;36m====================================================\033[0m")
		fmt.Print(" ➔ Nhap lua chon cua ban: ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			setupStaticIP(reader)
		case "2":
			manageUsers(reader)
		case "0":
			return
		default:
			fmt.Println("\033[0;31m❌ Lua chon khong hop le, vui long thu lai!\033[0m")
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')
		}
	}
}

func setupStaticIP(reader *bufio.Reader) {
	fmt.Println("\n\033[1;36m--- CAI DAT IP TINH ---\033[0m")

	// Tim interface mang dang hoat dong
	out, _ := utils.ExecCmd("ip", "-o", "link", "show")
	lines := strings.Split(out, "\n")
	var ifaces []string
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			name := strings.TrimSuffix(fields[1], ":")
			if name != "lo" && !strings.HasPrefix(name, "docker") && !strings.HasPrefix(name, "br-") && !strings.HasPrefix(name, "veth") {
				ifaces = append(ifaces, name)
			}
		}
	}

	if len(ifaces) == 0 {
		fmt.Println("   [!] Khong tim thay interface mang nao.")
		fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
		reader.ReadString('\n')
		return
	}

	fmt.Println("   Cac interface mang tim thay:")
	for i, iface := range ifaces {
		// Lay IP hien tai
		ipOut, _ := utils.ExecCmd("ip", "-4", "addr", "show", iface)
		currentIP := "N/A"
		for _, line := range strings.Split(ipOut, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "inet ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					currentIP = parts[1]
				}
			}
		}
		fmt.Printf("   %d. %s (IP hien tai: %s)\n", i+1, iface, currentIP)
	}

	fmt.Print("\n   Chon interface (nhap so thu tu): ")
	idxStr, _ := reader.ReadString('\n')
	idx, err := strconv.Atoi(strings.TrimSpace(idxStr))
	if err != nil || idx < 1 || idx > len(ifaces) {
		fmt.Println("   [!] Lua chon khong hop le.")
		fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
		reader.ReadString('\n')
		return
	}
	selectedIface := ifaces[idx-1]

	fmt.Printf("\n   Interface da chon: \033[1;32m%s\033[0m\n", selectedIface)
	fmt.Print("   Nhap IP tinh (VD: 192.168.1.100/24): ")
	ipAddr, _ := reader.ReadString('\n')
	ipAddr = strings.TrimSpace(ipAddr)

	fmt.Print("   Nhap Gateway (VD: 192.168.1.1): ")
	gateway, _ := reader.ReadString('\n')
	gateway = strings.TrimSpace(gateway)

	fmt.Print("   Nhap DNS (mac dinh 8.8.8.8, 1.1.1.1): ")
	dns, _ := reader.ReadString('\n')
	dns = strings.TrimSpace(dns)
	if dns == "" {
		dns = "8.8.8.8, 1.1.1.1"
	}

	if ipAddr == "" || gateway == "" {
		fmt.Println("   [!] Thieu thong tin IP hoac Gateway.")
		fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
		reader.ReadString('\n')
		return
	}

	dnsList := strings.Split(dns, ",")
	var dnsEntries []string
	for _, d := range dnsList {
		d = strings.TrimSpace(d)
		if d != "" {
			dnsEntries = append(dnsEntries, d)
		}
	}

	fmt.Println("\n   \033[1;33m--- XAC NHAN CAU HINH ---\033[0m")
	fmt.Printf("   Interface: %s\n", selectedIface)
	fmt.Printf("   IP:        %s\n", ipAddr)
	fmt.Printf("   Gateway:   %s\n", gateway)
	fmt.Printf("   DNS:       %s\n", strings.Join(dnsEntries, ", "))
	fmt.Print("\n   Ban co chac chan muon ap dung? (y/N): ")
	confirm, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
		fmt.Println("   [*] Da huy thao tac.")
		fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
		reader.ReadString('\n')
		return
	}

	if utils.IsDebian() {
		// Netplan (Ubuntu/Debian)
		dnsYaml := "[" + strings.Join(dnsEntries, ", ") + "]"
		netplanConfig := fmt.Sprintf(`network:
  version: 2
  ethernets:
    %s:
      dhcp4: no
      addresses:
        - %s
      routes:
        - to: default
          via: %s
      nameservers:
        addresses: %s
`, selectedIface, ipAddr, gateway, dnsYaml)

		netplanDir := "/etc/netplan/"
		entries, _ := os.ReadDir(netplanDir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
				os.Rename(netplanDir+e.Name(), netplanDir+e.Name()+".bak")
			}
		}
		os.WriteFile(netplanDir+"01-nalink-static.yaml", []byte(netplanConfig), 0600)
		fmt.Println("   [*] Dang ap dung cau hinh mang...")
		utils.ExecCmd("netplan", "apply")
	} else {
		// nmcli (CentOS/RHEL)
		// Remove old DHCP and set static
		utils.ExecCmd("nmcli", "con", "mod", selectedIface, "ipv4.method", "manual")
		utils.ExecCmd("nmcli", "con", "mod", selectedIface, "ipv4.addresses", ipAddr)
		utils.ExecCmd("nmcli", "con", "mod", selectedIface, "ipv4.gateway", gateway)
		utils.ExecCmd("nmcli", "con", "mod", selectedIface, "ipv4.dns", strings.Join(dnsEntries, ","))
		fmt.Println("   [*] Dang ap dung cau hinh mang...")
		utils.ExecCmd("nmcli", "con", "up", selectedIface)
	}

	fmt.Println("   \033[0;32m✔ Da cai dat IP tinh thanh cong!\033[0m")
	fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
	reader.ReadString('\n')
}

func manageUsers(reader *bufio.Reader) {
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Println("\n\033[1;36m--- QUAN LY USER ---\033[0m")

		// Liet ke user hien tai (UID >= 1000 va co shell dung duoc)
		content, _ := os.ReadFile("/etc/passwd")
		var users []string
		for _, line := range strings.Split(string(content), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 7 {
				uid, _ := strconv.Atoi(fields[2])
				shell := fields[6]
				if uid >= 1000 && uid < 65534 && (strings.Contains(shell, "bash") || strings.Contains(shell, "sh") || strings.Contains(shell, "zsh")) {
					// Kiem tra co phai sudo khong
					sudoTag := ""
					if err := exec.Command("id", "-nG", fields[0]).Run(); err == nil {
						groupOut, _ := utils.ExecCmd("id", "-nG", fields[0])
						if strings.Contains(groupOut, "sudo") || strings.Contains(groupOut, "wheel") {
							sudoTag = " \033[1;33m[SUDO]\033[0m"
						}
					}
					users = append(users, fields[0]+sudoTag)
				}
			}
		}

		fmt.Println("   Danh sach User he thong:")
		if len(users) == 0 {
			fmt.Println("   (Khong co user nao ngoai root)")
		}
		for i, u := range users {
			fmt.Printf("   %d. %s\n", i+1, u)
		}

		fmt.Println("\n   \033[1;33m1.\033[0m Them User moi")
		fmt.Println("   \033[1;33m2.\033[0m Xoa User")
		fmt.Println("   \033[1;33m3.\033[0m Cap quyen Sudo cho User")
		fmt.Println("   \033[1;33m4.\033[0m Doi mat khau User")
		fmt.Println("   \033[1;33m0.\033[0m Quay lai")
		fmt.Print("\n   ➔ Chon: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			fmt.Print("   Nhap ten User moi: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			if username == "" {
				continue
			}

			cmd := exec.Command("adduser", username)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("   [!] Loi tao User: %v\n", err)
			} else {
				fmt.Printf("   \033[0;32m✔ Da tao User '%s' thanh cong!\033[0m\n", username)
			}
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "2":
			fmt.Print("   Nhap ten User muon xoa: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			if username == "" || username == "root" {
				fmt.Println("   [!] Khong the xoa root hoac User rong.")
				fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
				reader.ReadString('\n')
				continue
			}
			fmt.Printf("   [!] Ban chac chan muon xoa User '%s' va toan bo du lieu? (y/N): ", username)
			confirm, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(confirm)) == "y" {
				utils.ExecCmd("userdel", "-r", username)
				fmt.Printf("   \033[0;32m✔ Da xoa User '%s'.\033[0m\n", username)
			} else {
				fmt.Println("   [*] Da huy thao tac.")
			}
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "3":
			fmt.Print("   Nhap ten User can cap quyen Sudo: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			if username == "" {
				continue
			}
			if utils.IsDebian() {
				utils.ExecCmd("usermod", "-aG", "sudo", username)
			} else {
				utils.ExecCmd("usermod", "-aG", "wheel", username)
			}
			fmt.Printf("   \033[0;32m✔ Da cap quyen Sudo cho '%s'.\033[0m\n", username)
			fmt.Print("\n(Nhan phim Enter de tiep tuc...)")
			reader.ReadString('\n')

		case "4":
			fmt.Print("   Nhap ten User can doi mat khau: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			if username == "" {
				continue
			}
			cmd := exec.Command("passwd", username)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("   [!] Loi doi mat khau: %v\n", err)
			} else {
				fmt.Printf("   \033[0;32m✔ Da doi mat khau cho '%s'.\033[0m\n", username)
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
