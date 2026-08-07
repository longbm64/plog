package caddy

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/DangLong/na-server-go/pkg/daemon"
	"github.com/DangLong/na-server-go/pkg/utils"
)

// runApt chay lenh apt voi DEBIAN_FRONTEND=noninteractive de tranh dialog tuong tac
func runApt(args ...string) {
	cmd := exec.Command("apt", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// InstallCaddy sets up Caddy server (supports Debian + RHEL)
func InstallCaddy(apiPort, vhostPort int, secToken string) error {
	fmt.Println("\n\033[0;34m>>> [70%] 5. Dang thiet lap lop giao tiep bao mat (Security Layer)...\033[0m")

	if utils.IsDebian() {
		installCaddyDebian()
	} else {
		installCaddyRHEL()
	}

	os.MkdirAll("/etc/caddy", 0755)
	os.MkdirAll("/var/www/html", 0755)

	domainFile := "/etc/caddy/domains.txt"
	if _, err := os.Stat(domainFile); os.IsNotExist(err) {
		os.WriteFile(domainFile, []byte(""), 0666)
	}

	// Cai Daemon API Go thay internal caddy-ask
	daemon.InstallDaemonService(apiPort, secToken)

	Generate404HTML()
	utils.ExecCmd("chown", "-R", "caddy:caddy", "/var/www/html")

	caddyConf := fmt.Sprintf(`{
    on_demand_tls {
        ask http://127.0.0.1:%d/check
    }
    servers {
        trusted_proxies static 127.0.0.1/8
    }
}

:443 {
    tls {
        on_demand
    }

    handle {
        reverse_proxy 127.0.0.1:%d {
            @error status 404 502
            handle_response @error {
                root * /var/www/html
                rewrite * /404.html
                file_server
            }
        }
    }

    handle_errors {
        root * /var/www/html
        rewrite * /404.html
        file_server
    }
}

:80 {
    redir https://{host}{uri} permanent
}
`, apiPort, vhostPort)

	fmt.Println("   - [81%] Dang thiet lap cac tham so truy cap bao mat...")
	os.WriteFile("/etc/caddy/Caddyfile", []byte(caddyConf), 0644)
	fmt.Println("\033[0;32m✔ Thanh cong\033[0m")
	return nil
}

func installCaddyDebian() {
	fmt.Println("   - [72%] Dang dong bo hoa tham so bao mat...")

	// Run silently
	stop := utils.ShowSpinner("Dang kich hoat luong truy cap an toan")
	cmd := exec.Command("apt", "install", "-y", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef", "debian-keyring", "debian-archive-keyring", "apt-transport-https", "curl", "gnupg")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.CombinedOutput()

	utils.ExecCmd("bash", "-c", "curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg")
	utils.ExecCmd("bash", "-c", "curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list")

	cmdUpd := exec.Command("apt", "update", "-y")
	cmdUpd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmdUpd.CombinedOutput()

	cmdInst := exec.Command("apt", "install", "-y", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef", "caddy")
	cmdInst.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a")
	cmdInst.CombinedOutput()
	stop()
}

func installCaddyRHEL() {
	pkgMgr := utils.GetPkgManager()

	stop := utils.ShowSpinner("Dang kich hoat luong truy cap an toan")
	exec.Command("bash", "-c", fmt.Sprintf("%s install -y 'dnf-command(copr)' 2>/dev/null || true", pkgMgr)).CombinedOutput()
	exec.Command("bash", "-c", "dnf core enable -y @caddy/caddy 2>/dev/null || true").CombinedOutput()

	repoContent := `[caddy]
name=NALink Web Server
baseurl=https://dl.cloudsmith.io/public/caddy/stable/rpm/el/$releasever/$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=https://dl.cloudsmith.io/public/caddy/stable/gpg.key
`
	os.WriteFile("/etc/yum.repos.d/caddy.repo", []byte(repoContent), 0644)

	utils.ExecCmd("rpm", "--import", "https://dl.cloudsmith.io/public/caddy/stable/gpg.key")

	utils.ExecCmd(pkgMgr, "clean", "all")
	exec.Command(pkgMgr, "install", "-y", "caddy").CombinedOutput()

	utils.ExecCmd("systemctl", "enable", "caddy")
	utils.ExecCmd("systemctl", "start", "caddy")
	stop()
}

// Generate404HTML builds the fallback domain page
func Generate404HTML() {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>NALink | 404</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;800;900&display=swap" rel="stylesheet">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        ::-webkit-scrollbar { display: none; width: 0; height: 0; }
        html, body { scrollbar-width: none; -ms-overflow-style: none; }
        body {
            min-height: 100vh;
            display: flex;
            font-family: 'Inter', system-ui, -apple-system, sans-serif;
            background: #0f172a;
            color: #e2e8f0;
            overflow-y: auto;
            overflow-x: hidden;
            position: relative;
            padding: 1rem;
        }
        
        html[lang="en"] [data-lang="vi"] { display: none !important; }
        html[lang="vi"] [data-lang="en"] { display: none !important; }

        .bg-grid {
            position: absolute;
            inset: 0;
            background-image: 
                linear-gradient(to right, rgba(255,255,255,0.03) 1px, transparent 1px),
                linear-gradient(to bottom, rgba(255,255,255,0.03) 1px, transparent 1px);
            background-size: 50px 50px;
            pointer-events: none;
        }
        .glow {
            position: fixed;
            width: 800px; height: 800px;
            border-radius: 50%;
            background: radial-gradient(circle, rgba(99,102,241,0.12), transparent 60%);
            top: 50%; left: 50%;
            transform: translate(-50%, -50%);
            pointer-events: none;
            animation: float 15s infinite ease-in-out alternate;
        }
        @keyframes float {
            0% { transform: translate(-50%, -40%) scale(1); }
            100% { transform: translate(-50%, -60%) scale(1.1); }
        }
        .container {
            position: relative;
            z-index: 10;
            text-align: center;
            padding: 2.5rem 1.5rem;
            max-width: 640px;
            margin: auto;
            background: rgba(30, 41, 59, 0.5);
            backdrop-filter: blur(24px);
            border: 1px solid rgba(255, 255, 255, 0.05);
            border-radius: 32px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
            animation: slideUp 0.8s cubic-bezier(0.16, 1, 0.3, 1) forwards;
            opacity: 0;
            transform: translateY(40px);
        }
        @keyframes slideUp {
            to { opacity: 1; transform: translateY(0); }
        }
        .logo-wrap {
            display: flex;
            justify-content: center;
            margin-bottom: 1.5rem;
        }
        .logo-img {
            width: 150px;
            height: 150px;
            object-fit: cover;
            border-radius: 28px;
            box-shadow: 0 12px 35px -8px rgba(129, 140, 248, 0.6);
            border: 1px solid rgba(255,255,255,0.15);
        }
        .error-badge {
            display: inline-flex;
            align-items: center;
            padding: 0.5rem 1.25rem;
            background: rgba(239, 68, 68, 0.1);
            border: 1px solid rgba(239, 68, 68, 0.2);
            color: #fca5a5;
            border-radius: 999px;
            font-size: 0.875rem;
            font-weight: 600;
            margin-bottom: 1rem;
            letter-spacing: 0.1em;
            text-transform: uppercase;
        }
        .title {
            font-size: 1.75rem;
            font-weight: 600;
            color: #f8fafc;
            margin-bottom: 1rem;
            line-height: 1.3;
        }
        .desc {
            color: #94a3b8;
            font-size: 1.05rem;
            line-height: 1.6;
            margin-bottom: 1.5rem;
            padding: 0 1rem;
        }
        .divider {
            height: 1px;
            background: linear-gradient(90deg, transparent, rgba(255,255,255,0.1), transparent);
            margin: 1.5rem 0;
        }
        .features {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 1rem;
            text-align: left;
        }
        .feature-item {
            display: flex;
            align-items: flex-start;
            gap: 0.875rem;
            padding: 1.25rem 1rem;
            border-radius: 16px;
            background: rgba(255,255,255,0.02);
            transition: background 0.3s ease;
            border: 1px solid transparent;
        }
        .feature-item:hover {
            background: rgba(255,255,255,0.04);
            border-color: rgba(255,255,255,0.05);
        }
        .feature-icon {
            color: #818cf8;
            flex-shrink: 0;
            margin-top: 2px;
        }
        .feature-text h4 {
            color: #e2e8f0;
            font-size: 0.95rem;
            font-weight: 600;
            margin-bottom: 0.25rem;
        }
        .feature-text p {
            color: #64748b;
            font-size: 0.85rem;
            line-height: 1.5;
        }
        .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            gap: 10px;
            padding: 0.875rem 2rem;
            background: linear-gradient(135deg, #4f46e5, #7c3aed);
            color: white;
            text-decoration: none;
            border-radius: 14px;
            font-weight: 600;
            font-size: 1rem;
            transition: all 0.3s ease;
            border: 1px solid rgba(255,255,255,0.1);
            position: relative;
            overflow: hidden;
        }
        .btn::after {
            content: '';
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            background: linear-gradient(135deg, rgba(255,255,255,0.2), transparent);
            opacity: 0;
            transition: opacity 0.3s ease;
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 25px -5px rgba(79, 70, 229, 0.4);
        }
        .btn:hover::after {
            opacity: 1;
        }
        .footer {
            margin-top: 1.5rem;
            font-size: 0.85rem;
            color: #475569;
            font-weight: 500;
        }
        .footer a {
            color: #64748b;
            text-decoration: none;
            transition: color 0.2s ease;
        }
        .footer a:hover {
            color: #94a3b8;
            text-decoration: underline;
        }
        
        @media (max-width: 640px) {
            .container { padding: 3rem 1.5rem; border-radius: 24px; }
            .logo-img { width: 150px; height: 150px; border-radius: 20px; }
            .features { grid-template-columns: 1fr; gap: 1rem; }
            .desc { padding: 0; }
        }
    </style>
</head>
<body>
    <script>
        (function() {
            var userLang = navigator.language || navigator.userLanguage || 'en';
            if (userLang.toLowerCase().startsWith('vi')) {
                document.documentElement.lang = 'vi';
                document.title = 'NALink | 404 Không Tìm Thấy';
            } else {
                document.documentElement.lang = 'en';
                document.title = 'NALink | 404 Not Found';
            }
        })();
    </script>
    
    <div class="bg-grid"></div>
    <div class="glow"></div>
    <div class="container">
        
        <div class="logo-wrap">
            <img src="https://nalink.app/logo.png" alt="NALink Logo" class="logo-img">
        </div>

        <!-- Tiếng Việt -->
        <div data-lang="vi">
            <div class="error-badge">404 - Không Tìm Thấy Kết Nối</div>
            <p class="desc">Hệ thống NALink không tìm thấy kết nối cho tên miền này.</p>
        </div>

        <!-- English -->
        <div data-lang="en">
            <div class="error-badge">404 - CONNECTION NOT FOUND</div>
            <p class="desc">No active Tunnel stream could be found for this domain.<br>Your client device might be in an offline state.</p>
        </div>
        
        <div class="divider"></div>

        <div class="features">
            <!-- Feature 1 -->
            <div class="feature-item">
                <svg class="feature-icon" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"></path></svg>
                <div class="feature-text" data-lang="vi">
                    <h4>Bảo Mật Tuyệt Đối</h4>
                    <p>Tunnel an toàn với tiêu chuẩn mã hóa End-to-End.</p>
                </div>
                <div class="feature-text" data-lang="en">
                    <h4>Absolute Security</h4>
                    <p>Secure tunneling with End-to-End encryption.</p>
                </div>
            </div>
            
            <!-- Feature 2 -->
            <div class="feature-item">
                <svg class="feature-icon" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"></path></svg>
                <div class="feature-text" data-lang="vi">
                    <h4>Hiệu Suất Cao</h4>
                    <p>Hoạt động ổn định liên tục trên hạ tầng CloudVPS.</p>
                </div>
                <div class="feature-text" data-lang="en">
                    <h4>High Performance</h4>
                    <p>Continuous stable operation on CloudVPS infrastructure.</p>
                </div>
            </div>
        </div>

        <div style="margin-top: 1.5rem;">
            <a href="https://nalink.app" class="btn">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path><polyline points="9 22 9 12 15 12 15 22"></polyline></svg>
                <span data-lang="vi">Trang Chủ NALink</span>
                <span data-lang="en">NALink Homepage</span>
            </a>
        </div>

        <div class="footer">
            <span data-lang="vi">&copy; 2026 NALink. Được phát triển bởi <a href="https://kho24h.com">kho24h.com</a></span>
            <span data-lang="en">&copy; 2026 NALink. Developed by <a href="https://kho24h.com">kho24h.com</a></span>
        </div>
    </div>
</body>
</html>`
	os.WriteFile("/var/www/html/404.html", []byte(html), 0644)
}
