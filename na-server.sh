# 1. Tạo thư mục chứa tool nếu chưa có
sudo mkdir -p /usr/local/bin

# 2. GHI TRỰC TIẾP TOÀN BỘ SCRIPT VÀO ĐÍCH (/usr/local/bin/na)
sudo tee /usr/local/bin/na >/dev/null << 'END_OF_SCRIPT'
#!/bin/bash
# =============================================
# TOOL QUẢN LÝ VPS - FRP + Caddy + On-Demand TLS
# =============================================
if [[ $EUID -ne 0 ]]; then
    echo -e "\033[0;31m❌ Script này phải chạy với quyền root (sudo)\033[0m"
    exit 1
fi

DOMAIN_FILE="/etc/caddy/domains.txt"
CADDY_CONF="/etc/caddy/Caddyfile"
INFO_FILE="$HOME/thong_tin_vps.txt"
FRP_CONF="/etc/frp/frps.toml"
SSH_CONF="/etc/ssh/sshd_config"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

init_files() {
    mkdir -p /etc/caddy /etc/frp /var/www/html
    [ ! -f "$DOMAIN_FILE" ] && touch "$DOMAIN_FILE"
    chmod 666 "$DOMAIN_FILE"
}

get_ssh_port() {
    grep -E "^#?Port " "$SSH_CONF" | awk '{print $2}' | head -n1 || echo "22"
}

get_free_port() {
    while true; do
        PORT=$((RANDOM % 50000 + 10000))
        if ! ss -tuln | grep -q ":$PORT "; then
            echo "$PORT"
            return
        fi
    done
}

update_system() {
    echo -e "\n${YELLOW}>>> Đang cập nhật hệ thống...${NC}"
    apt update -y && apt upgrade -y
    apt install -y curl jq python3 ufw fail2ban unzip zip
}

setup_swap() {
    echo -e "\n${YELLOW}>>> Đang kiểm tra và thiết lập Swap...${NC}"
    RAM_MB=$(free -m | awk '/^Mem:/ {print $2}')
    if free | awk '/^Swap:/ {print $2}' | grep -q "^0$"; then
        if [ "$RAM_MB" -le 2500 ]; then SWAP_SIZE=2048
        elif [ "$RAM_MB" -le 6000 ]; then SWAP_SIZE=4096
        else SWAP_SIZE=4096
        fi
        echo -e "${YELLOW}Đang tạo Swap ${SWAP_SIZE}MB...${NC}"
        fallocate -l ${SWAP_SIZE}M /swapfile
        chmod 600 /swapfile
        mkswap /swapfile
        swapon /swapfile
        echo '/swapfile none swap sw 0 0' >> /etc/fstab
        echo -e "${GREEN}✔ Đã tạo Swap ${SWAP_SIZE}MB${NC}"
    else
        SWAP_MB=$(free -m | awk '/^Swap:/ {print $2}')
        echo -e "${GREEN}✔ Đã có Swap (${SWAP_MB}MB)${NC}"
    fi
}

security_harden() {
    echo -e "\n${YELLOW}>>> Đang tăng cường bảo mật VPS...${NC}"
    NEW_SSH_PORT=$(shuf -i 2000-65000 -n 1)
    echo -e "${YELLOW}Đang đổi SSH port sang $NEW_SSH_PORT...${NC}"
    sed -i "s/^#\?Port .*/Port $NEW_SSH_PORT/" "$SSH_CONF"
    
    echo -e "${YELLOW}Đang thiết lập thông số bảo mật an toàn cho SSH...${NC}"
    CONFIGS=(
        "PermitRootLogin no"
        "PermitEmptyPasswords no"
        "MaxAuthTries 3"
        "ClientAliveInterval 300"
        "ClientAliveCountMax 2"
        "X11Forwarding no"
        "UseDNS no"
        "LoginGraceTime 60"
    )
    for config in "${CONFIGS[@]}"; do
        key=$(echo "$config" | awk '{print $1}')
        if grep -qE "^[#]*[[:space:]]*$key" "$SSH_CONF" 2>/dev/null; then
            sed -i -E "s/^[#]*[[:space:]]*$key.*/$config/" "$SSH_CONF"
        else
            echo "$config" >> "$SSH_CONF"
        fi
        
        # Xử lý Override Config (ví dụ 50-cloud-init.conf) trên Ubuntu
        if ls /etc/ssh/sshd_config.d/*.conf >/dev/null 2>&1; then
            for f in /etc/ssh/sshd_config.d/*.conf; do
                sed -i -E "s/^[#]*[[:space:]]*$key.*/$config/" "$f" 2>/dev/null || true
            done
        fi
    done

    
    cat > /etc/fail2ban/jail.local <<EOF_F2B
[sshd]
enabled = true
port = ${NEW_SSH_PORT}
filter = sshd
logpath = /var/log/auth.log
maxretry = 4
bantime = 3600
findtime = 600
ignoreip = 127.0.0.1/8 ::1
EOF_F2B
    systemctl restart fail2ban

    IP=$(curl -s ifconfig.me || echo "Không lấy được")
    CPU_CORES=$(nproc)
    RAM_TOTAL=$(free -h | awk '/^Mem:/ {print $2}')
    SWAP_TOTAL=$(free -h | awk '/^Swap:/ {print $2}' || echo "0B")
    
    cat > "$INFO_FILE" <<EOF_INFO
=============================
    THÔNG TIN VPS - FRP + Caddy
=============================
CPU Cores       : $CPU_CORES
RAM             : $RAM_TOTAL
Swap RAM        : $SWAP_TOTAL
IP VPS          : $IP
SSH Port        : $NEW_SSH_PORT
FRP Bind Port   : $B_PORT
FRP Token       : $TOKEN
VHOST Port      : $V_PORT
API Port        : $API_PORT
API Secure Token: $SEC_TOKEN
Domain File     : $DOMAIN_FILE
=============================
[API DOMAIN] LỆNH THÊM TỪ CLIENT:
curl -X POST -d "token=$SEC_TOKEN&domain=yourdomain.com" http://$IP:$API_PORT
[API DOMAIN] LỆNH XÓA TỪ CLIENT:
curl -X POST -d "token=$SEC_TOKEN&domain=yourdomain.com&action=delete" http://$IP:$API_PORT
[API PORT UFW] LỆNH MỞ CỔNG TỪ CLIENT:
curl -X POST -d "token=$SEC_TOKEN&port=6000&action=add_port" http://$IP:$API_PORT
[API PORT UFW] LỆNH ĐÓNG CỔNG TỪ CLIENT:
curl -X POST -d "token=$SEC_TOKEN&port=6000&action=delete_port" http://$IP:$API_PORT
=============================
EOF_INFO

    echo -e "${YELLOW}Đang cấu hình UFW Firewall...${NC}"
    ufw --force reset >/dev/null 2>&1
    ufw allow 80/tcp
    ufw allow 443/tcp
    ufw allow "${API_PORT}"/tcp
    ufw allow "${NEW_SSH_PORT}"/tcp
    ufw allow "${B_PORT}"/tcp
    ufw allow from 127.0.0.1 to any port "${V_PORT}" proto tcp
    ufw deny "${V_PORT}"/tcp
    ufw --force enable
    
    systemctl restart ssh
    systemctl restart ssh.socket 2>/dev/null || true
    echo -e "${GREEN}✔ Hoàn tất bảo mật VPS${NC}"
}

install_frp() {
    echo -e "\n${BLUE}>>> Đang cài đặt FRP Server...${NC}"
    FRP_VER=$(curl -s https://api.github.com/repos/fatedier/frp/releases/latest | jq -r .tag_name | sed 's/v//')
    wget -q https://github.com/fatedier/frp/releases/download/v${FRP_VER}/frp_${FRP_VER}_linux_amd64.tar.gz -O /tmp/frp.tar.gz
    tar -zxf /tmp/frp.tar.gz -C /tmp
    mv /tmp/frp_${FRP_VER}_linux_amd64/frps /usr/local/bin/frps
    chmod +x /usr/local/bin/frps
    
    cat > "$FRP_CONF" <<EOF_FRP
bindAddr = "0.0.0.0"
bindPort = ${B_PORT}
vhostHTTPPort = ${V_PORT}

auth.method = "token"
auth.token = "${TOKEN}"

custom404Page = "/var/www/html/404.html"
EOF_FRP

    cat > /etc/systemd/system/frps.service <<EOF_FRP_SVC
[Unit]
Description=FRP Server
After=network.target

[Service]
ExecStart=/usr/local/bin/frps -c ${FRP_CONF}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF_FRP_SVC
}

install_caddy() {
    echo -e "\n${BLUE}>>> Đang cài đặt Caddy và API kiểm tra Domain/Port...${NC}"
    apt install -y debian-keyring debian-archive-keyring apt-transport-https curl gnupg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list > /dev/null
    apt update -y
    apt install -y caddy
    
    cat > /usr/local/bin/caddy-ask.py << EOF_PY
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs
import os
import subprocess

DOMAIN_FILE = "/etc/caddy/domains.txt"
SECURE_TOKEN = "${SEC_TOKEN}"

class AskHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        qs = parse_qs(urlparse(self.path).query)
        domain = qs.get("domain", [""])[0]
        if not domain or not os.path.exists(DOMAIN_FILE):
            self.send_response(403)
            self.end_headers()
            return
        with open(DOMAIN_FILE, "r") as f:
            domains = [line.strip() for line in f.readlines()]
        if domain in domains:
            self.send_response(200)
        else:
            self.send_response(403)
        self.end_headers()

    def do_POST(self):
        content_length = int(self.headers.get('Content-Length', 0))
        if content_length == 0:
            self.send_response(400)
            self.end_headers()
            return
            
        post_data = self.rfile.read(content_length).decode('utf-8')
        data = parse_qs(post_data)
        
        token = data.get("token", [""])[0]
        domain = data.get("domain", [""])[0]
        port = data.get("port", [""])[0]
        action = data.get("action", ["add"])[0]

        if token == SECURE_TOKEN:
            # === XỬ LÝ MỞ/ĐÓNG PORT UFW ===
            if port:
                if not port.isdigit():
                    self.send_response(400)
                    self.end_headers()
                    self.wfile.write(b"Invalid port format")
                    return
                
                if action == "delete_port":
                    subprocess.run(["ufw", "delete", "allow", port + "/tcp"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b"Port closed successfully")
                else:
                    # Kiểm tra trùng lặp Port
                    ufw_status = subprocess.run(["ufw", "status"], capture_output=True, text=True)
                    if f"{port}/tcp" in ufw_status.stdout:
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Port already opened")
                    else:
                        subprocess.run(["ufw", "allow", port + "/tcp"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Port opened successfully")
                    
            # === XỬ LÝ THÊM/XÓA DOMAIN ===
            elif domain:
                domain = domain.strip().lower()
                
                if os.path.exists(DOMAIN_FILE):
                    with open(DOMAIN_FILE, "r") as f:
                        existing = [line.strip() for line in f.readlines() if line.strip()]
                else:
                    existing = []

                if action == "delete":
                    if domain in existing:
                        existing.remove(domain)
                        with open(DOMAIN_FILE, "w") as f:
                            for d in existing:
                                f.write(d + "\n")
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Domain deleted successfully")
                    else:
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Domain not found in list")
                else:
                    # Kiểm tra trùng lặp Domain
                    if domain in existing:
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Domain already exists")
                    else:
                        with open(DOMAIN_FILE, "a") as f:
                            f.write(domain + "\n")
                        self.send_response(200)
                        self.end_headers()
                        self.wfile.write(b"Domain added successfully")
            else:
                self.send_response(400)
                self.end_headers()
                self.wfile.write(b"Missing domain or port parameter")
        else:
            self.send_response(401)
            self.end_headers()

    def log_message(self, format, *args):
        pass

HTTPServer(("0.0.0.0", ${API_PORT}), AskHandler).serve_forever()
EOF_PY

    cat > /etc/systemd/system/caddy-ask.service << 'EOF_PY_SVC'
[Unit]
Description=Caddy Ask API
After=network.target

[Service]
ExecStart=/usr/bin/python3 /usr/local/bin/caddy-ask.py
Restart=always
User=root

[Install]
WantedBy=multi-user.target
EOF_PY_SVC

    generate_default_html
    generate_404_html
    chown -R caddy:caddy /var/www/html

    SERVER_IP="$(curl -s ifconfig.me 2>/dev/null || true)"
    HOMEPAGE_HOSTS="localhost 127.0.0.1"
    if [ -n "$SERVER_IP" ]; then
        HOMEPAGE_HOSTS="$HOMEPAGE_HOSTS $SERVER_IP"
    fi

    cat > "$CADDY_CONF" <<EOF_CADDY
{
    on_demand_tls {
        ask http://127.0.0.1:${API_PORT}/check
    }
    servers {
        trusted_proxies static 127.0.0.1/8
    }
}

:80, :443 {
    tls {
        on_demand
    }

    @homepage host $HOMEPAGE_HOSTS
    handle @homepage {
        root * /var/www/html
        file_server
    }

    handle {
        reverse_proxy 127.0.0.1:${V_PORT}
    }
}
EOF_CADDY

    echo -e "${GREEN}✔ Cài đặt Caddy và API thành công${NC}"
}

generate_404_html() {
    cat > /var/www/html/404.html << 'EOF_404'
<!DOCTYPE html>
<html lang="vi">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404 | Không tìm thấy</title>
    <meta name="description" content="Domain hoặc URL hiện tại không tồn tại hoặc chưa được cấu hình.">
    <style>
        :root {
            --bg1: #0f172a;
            --bg2: #1e293b;
            --acc1: #60a5fa;
            --acc2: #a78bfa;
            --danger1: #fb7185;
            --danger2: #f97316;
            --text: #e5e7eb;
            --muted: #94a3b8;
            --card: rgba(255, 255, 255, 0.06);
            --border: rgba(255, 255, 255, 0.12);
        }

        html,
        body {
            height: 100%;
            overflow: hidden;
        }

        body {
            margin: 0;
            font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", "Apple Color Emoji", "Segoe UI Emoji";
            color: var(--text);
            background: radial-gradient(1200px 600px at 20% 10%, var(--bg2), transparent 60%), linear-gradient(135deg, var(--bg1), #111827 60%);
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 24px;
        }

        .wrap {
            width: 100%;
            max-width: 720px;
        }

        .card {
            background: var(--card);
            border: 1px solid var(--border);
            border-radius: 20px;
            padding: 40px 28px;
            backdrop-filter: saturate(140%) blur(10px);
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
            text-align: center;
        }

        .badge {
            display: inline-flex;
            align-items: center;
            gap: 10px;
            padding: 8px 12px;
            border-radius: 999px;
            border: 1px solid rgba(248, 113, 113, 0.35);
            background: rgba(248, 113, 113, 0.12);
            color: #fecdd3;
            font-weight: 700;
            letter-spacing: 0.04em;
            font-size: 12px;
            text-transform: uppercase;
        }

        .title {
            font-size: clamp(25px, 5vw, 42px);
            line-height: 1.1;
            margin: 14px 0 10px;
            background: linear-gradient(90deg, var(--danger1), var(--danger2));
            -webkit-background-clip: text;
            background-clip: text;
            color: transparent;
        }

        .subtitle {
            font-size: clamp(14px, 2.2vw, 18px);
            color: var(--muted);
            margin: 0 0 20px;
        }

        .info {
            display: grid;
            gap: 10px;
            justify-items: center;
            margin-top: 10px;
        }

        .row {
            width: min(560px, 100%);
            padding: 10px 12px;
            border-radius: 12px;
            border: 1px solid rgba(255, 255, 255, 0.10);
            background: rgba(255, 255, 255, 0.04);
            text-align: left;
            display: grid;
            gap: 4px;
        }

        .label {
            font-size: 12px;
            color: var(--muted);
        }

        .value {
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
            font-size: 14px;
            color: #e2e8f0;
            word-break: break-word;
        }

        .actions {
            display: flex;
            gap: 10px;
            justify-content: center;
            margin-top: 22px;
            flex-wrap: wrap;
        }

        .btn {
            appearance: none;
            border: 1px solid rgba(255, 255, 255, 0.16);
            background: rgba(255, 255, 255, 0.06);
            color: var(--text);
            padding: 10px 14px;
            border-radius: 12px;
            cursor: pointer;
            font-weight: 600;
            text-decoration: none;
            transition: transform 120ms ease, background 120ms ease, border-color 120ms ease;
        }

        .btn:hover {
            transform: translateY(-1px);
            background: rgba(255, 255, 255, 0.10);
            border-color: rgba(255, 255, 255, 0.22);
        }

        .btn.primary {
            border-color: rgba(96, 165, 250, 0.35);
            background: rgba(96, 165, 250, 0.14);
        }

        .btn.primary:hover {
            background: rgba(96, 165, 250, 0.18);
            border-color: rgba(96, 165, 250, 0.45);
        }

        .footer {
            margin-top: 26px;
            font-size: 14px;
            color: var(--muted);
        }

        .brand {
            color: #c7d2fe;
            text-decoration: none;
            border-bottom: 1px dashed rgba(167, 139, 250, 0.4);
        }

        .brand:hover {
            color: #e9d5ff;
            border-bottom-color: rgba(233, 213, 255, 0.7);
        }
    </style>
</head>

<body>
    <main class="wrap">
        <section class="card" role="region" aria-label="Trang 404">
            <div class="badge">404 Not Found</div>
            <h3 class="title">Không tìm thấy nội dung</h3>
            <p class="subtitle">Domain hoặc URL hiện tại không tồn tại, chưa được cấu hình, hoặc đã bị thay đổi.</p>

            <div class="info" aria-live="polite">
                <div class="row">
                    <div class="label">Domain</div>
                    <div id="host" class="value">--</div>
                </div>
                <div class="row">
                    <div class="label">URL</div>
                    <div id="url" class="value">--</div>
                </div>
            </div>

            <div class="actions">
                <button class="btn" type="button" id="reload">Thử lại</button>
            </div>

            <p class="footer">Phát triển bởi <a class="brand" href="https://kho24h.com" target="_blank"
                    rel="noopener noreferrer">kho24h.com</a></p>
        </section>
    </main>

    <script>
        function setText(id, value) {
            var el = document.getElementById(id);
            if (el) el.textContent = value;
        }
        document.addEventListener('DOMContentLoaded', function () {
            setText('host', window.location.hostname || '--');
            setText('url', window.location.href || '--');
            var btn = document.getElementById('reload');
            if (btn) btn.addEventListener('click', function () { window.location.reload(); });
        });
    </script>
</body>

</html>
EOF_404

    # Đảm bảo FRP biết cấu hình 404 này (Dành cho việc update nóng không cần cài lại)
    if [ -f "$FRP_CONF" ] && ! grep -q "custom404Page" "$FRP_CONF"; then
        echo 'custom404Page = "/var/www/html/404.html"' >> "$FRP_CONF"
        systemctl restart frps 2>/dev/null || true
    fi
}

generate_default_html() {
    cat > /var/www/html/index.html << 'EOF_HTML'
<!DOCTYPE html>
<html lang="vi">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Chào mừng | kho24h.com</title>
    <style>
        :root {
            --bg1: #0f172a; --bg2: #1e293b; --acc1: #60a5fa; --acc2: #a78bfa;
            --text: #e5e7eb; --muted: #94a3b8; --card: rgba(255,255,255,0.06); --border: rgba(255,255,255,0.12);
        }
        body {
            margin: 0; font-family: ui-sans-serif, system-ui, -apple-system, sans-serif;
            color: var(--text); height: 100vh; display: flex; align-items: center; justify-content: center;
            background: radial-gradient(1200px 600px at 20% 10%, var(--bg2), transparent 60%), linear-gradient(135deg, var(--bg1), #111827 60%);
        }
        .card {
            background: var(--card); border: 1px solid var(--border); border-radius: 20px;
            padding: 40px 28px; text-align: center; backdrop-filter: blur(10px); box-shadow: 0 10px 30px rgba(0,0,0,0.3);
        }
        .title {
            font-size: clamp(28px, 5vw, 42px); margin: 0 0 12px;
            background: linear-gradient(90deg, var(--acc1), var(--acc2)); -webkit-background-clip: text; color: transparent;
        }
        .subtitle { color: var(--muted); margin: 0 0 28px; }
        .time { font-family: monospace; font-size: clamp(34px, 8vw, 64px); font-weight: bold; color: #fff; }
    </style>
</head>
<body>
    <div class="card">
        <h1 class="title">Hệ Thống Trực Tuyến</h1>
        <p class="subtitle">Máy chủ VPS đã sẵn sàng để tiếp nhận kết nối.</p>
        <div class="time" id="time">--:--:--</div>
    </div>
    <script>
        setInterval(() => {
            document.getElementById('time').textContent = new Date().toLocaleTimeString('vi-VN', {hour12: false});
        }, 1000);
    </script>
</body>
</html>
EOF_HTML
}

update_homepage() {
    while true; do
        echo -e "\n${CYAN}=== CẬP NHẬT TRANG CHỦ MỚI (TRUY CẬP IP) ===${NC}"
        echo "1. Chỉnh sửa code thủ công (Dùng Nano)"
        echo "2. Cập nhật qua URL (Nhận file .zip hoặc .html)"
        echo "3. Khôi phục trang chủ mặc định"
        echo "4. Quay lại menu chính"
        read -p "Chọn: " hc
        case $hc in
            1)
                nano /var/www/html/index.html
                chown -R caddy:caddy /var/www/html
                echo -e "${GREEN}✔ Đã lưu thay đổi vào index.html!${NC}"
                ;;
            2)
                read -p "Nhập đường dẫn URL: " url
                if [[ -z "$url" ]]; then echo -e "${RED}URL không được trống!${NC}"; continue; fi
                if [[ "$url" == *.zip ]]; then
                    wget -qO /tmp/web.zip "$url"
                    rm -f /var/www/html/index.html
                    unzip -q -o /tmp/web.zip -d /var/www/html/
                    rm -f /tmp/web.zip
                    DIR_NAME=$(ls -A /var/www/html)
                    if [ $(echo "$DIR_NAME" | wc -l) -eq 1 ] && [ -d "/var/www/html/$DIR_NAME" ]; then
                        mv /var/www/html/"$DIR_NAME"/* /var/www/html/
                        rm -rf /var/www/html/"$DIR_NAME"
                    fi
                else
                    wget -qO /var/www/html/index.html "$url"
                fi
                chown -R caddy:caddy /var/www/html
                echo -e "${GREEN}✔ Đã cập nhật xong mã nguồn!${NC}"
                ;;
            3)
                generate_default_html
                chown -R caddy:caddy /var/www/html
                echo -e "${GREEN}✔ Đã khôi phục trang chủ mặc định!${NC}"
                ;;
            4) return ;;
            *) echo -e "${RED}Không hợp lệ${NC}" ;;
        esac
    done
}

update_404page() {
    while true; do
        echo -e "\n${CYAN}=== CẬP TRANG LỖI 404 (FRP OFFLINE) ===${NC}"
        echo "1. Chỉnh sửa code thủ công (Dùng Nano)"
        echo "2. Cập nhật qua URL (.html)"
        echo "3. Khôi phục giao diện 404 mặc định"
        echo "4. Quay lại menu chính"
        read -p "Chọn: " fhc
        case $fhc in
            1)
                nano /var/www/html/404.html
                echo -e "${GREEN}✔ Đã lưu thay đổi vào 404.html!${NC}"
                ;;
            2)
                read -p "Nhập đường dẫn URL tới file .html: " url
                if [[ -n "$url" ]]; then
                    wget -qO /var/www/html/404.html "$url"
                    echo -e "${GREEN}✔ Đã thay thế 404.html thành công!${NC}"
                fi
                ;;
            3)
                generate_404_html
                echo -e "${GREEN}✔ Đã khôi phục trang 404 mặc định!${NC}"
                ;;
            4) return ;;
            *) echo -e "${RED}Không hợp lệ${NC}" ;;
        esac
    done
}

start_services() {
    echo -e "\n${YELLOW}>>> Khởi động dịch vụ...${NC}"
    systemctl daemon-reload
    systemctl enable caddy-ask frps caddy
    systemctl restart caddy-ask frps caddy
    sleep 2
    check_status
}

check_status() {
    echo -e "\n${CYAN}--- TRẠNG THÁI HỆ THỐNG ---${NC}"
    echo -e "FRP   : $(systemctl is-active frps 2>/dev/null || echo "${RED}DỪNG${NC}")"
    echo -e "Caddy : $(systemctl is-active caddy 2>/dev/null || echo "${RED}DỪNG${NC}")"
    echo -e "API   : $(systemctl is-active caddy-ask 2>/dev/null || echo "${RED}DỪNG${NC}")"
}

manage_domains() {
    echo -e "\n${CYAN}=== QUẢN LÝ DOMAIN ===${NC}"
    while true; do
        echo "1. Xem danh sách"
        echo "2. Thêm domain"
        echo "3. Xóa domain"
        echo "4. Quay lại"
        read -p "Chọn: " c
        case $c in
            1) echo -e "${GREEN}Danh sách:${NC}"; cat "$DOMAIN_FILE" 2>/dev/null || echo "Chưa có domain" ;;
            2) read -p "Nhập domain: " d; echo "$d" | tee -a "$DOMAIN_FILE" >/dev/null; echo -e "${GREEN}✔ Đã thêm${NC}" ;;
            3) read -p "Nhập domain cần xóa: " d; sed -i "/^$d$/d" "$DOMAIN_FILE"; echo -e "${GREEN}✔ Đã xóa${NC}" ;;
            4) break ;;
            *) echo -e "${RED}Không hợp lệ${NC}" ;;
        esac
    done
}

manage_ports() {
    echo -e "\n${CYAN}=== QUẢN LÝ PORT (UFW FIREWALL) ===${NC}"
    while true; do
        echo "1. Xem danh sách Port đang mở (Kèm dịch vụ)"
        echo "2. Mở Port mới"
        echo "3. Đóng Port"
        echo "4. Quay lại"
        read -p "Chọn: " c
        case $c in
            1) 
                echo -e "\n${GREEN}Danh sách Port UFW & Dịch vụ:${NC}"
                printf "%-6s | %-12s | %-10s | %s\n" "[ID]" "PORT" "ACTION" "DỊCH VỤ ĐANG CHẠY"
                echo "-------------------------------------------------------------"
                ufw status numbered | grep -E '^\[[ 0-9]+\]' | while read -r line; do
                    id=$(echo "$line" | awk -F'[][]' '{print $2}' | tr -d ' ')
                    port_proto=$(echo "$line" | awk '{print $2}')
                    action=$(echo "$line" | awk '{print $3}')
                    port=$(echo "$port_proto" | cut -d'/' -f1)
                    
                    if [[ "$port" =~ ^[0-9]+$ ]]; then
                        # Lấy tên tiến trình đang chiếm port
                        svc=$(ss -tulpn 2>/dev/null | grep -E ":$port\b" | awk -F'"' '{print $2}' | head -n1)
                        [ -z "$svc" ] && svc="${YELLOW}-- Trống --${NC}" || svc="${CYAN}$svc${NC}"
                    else
                        svc="N/A"
                    fi
                    printf "[%2s]   | %-12s | %-10s | %b\n" "$id" "$port_proto" "$action" "$svc"
                done
                ;;
            2) read -p "Nhập Port cần mở (VD: 6000): " p; ufw allow "$p"/tcp; echo -e "${GREEN}✔ Đã mở Port $p${NC}" ;;
            3) read -p "Nhập Port cần đóng (VD: 6000): " p; ufw delete allow "$p"/tcp; echo -e "${GREEN}✔ Đã đóng Port $p${NC}" ;;
            4) break ;;
            *) echo -e "${RED}Không hợp lệ${NC}" ;;
        esac
    done
}

show_install_info() {
    echo -e "\n${CYAN}========== THÔNG TIN SẼ CÀI ĐẶT ==========${NC}"
    echo -e "1. Cập nhật hệ thống"
    echo -e "2. Cài FRP Server (mới nhất chuẩn TOML)"
    echo -e "3. Cài Caddy + API Check Domain + Web 404/Homepage"
    echo -e "4. Cấu hình On-Demand TLS chuẩn xác"
    echo -e "5. Tăng cường bảo mật UFW & Swap thông minh"
    echo -e "\n${YELLOW}Port sẽ sử dụng:"
    echo -e "   • FRP Bind Port     : $B_PORT"
    echo -e "   • VHOST Port        : $V_PORT"
    echo -e "   • API Port          : $API_PORT"
    echo -e "   • SSH Port mới      : Sẽ đổi ngẫu nhiên${NC}"
    echo -e "${CYAN}========================================${NC}"
}

# ===== UPDATE SCRIPT =====
update_script() {
    echo -e "\n${CYAN}--- CẬP NHẬT TOOL QUẢN LÝ ---${NC}"
    echo -e "${YELLOW}Đang tải phiên bản mới nhất từ GitHub...${NC}"
    
    UPDATE_URL="https://longbm64.github.io/plog/vps/na-server.sh" # HOẶC na-server.sh
    TMP_FILE="/tmp/na-update.sh"

    if curl -s -f -L "$UPDATE_URL" -o "$TMP_FILE"; then
        if grep -q "bash" "$TMP_FILE"; then
            echo -e "${GREEN}✔ Tải mã nguồn thành công! Tiến hành cài đặt bản mới...${NC}"
            sleep 1
            
            # 1. Chạy file cài đặt vừa tải về (Nó sẽ tự ghi đè tool và tự gọi lệnh 'na' ở cuối)
            bash "$TMP_FILE"
            
            # 2. Kill luôn tiến trình cũ này đi để nhường sân khấu cho bản mới
            exit 0
        else
            echo -e "${RED}❌ Lỗi: Nội dung file tải về không hợp lệ!${NC}"
            rm -f "$TMP_FILE"
            sleep 2
        fi
    else
        echo -e "${RED}❌ Lỗi: Không thể tải mã nguồn từ URL. Vui lòng kiểm tra lại kết nối mạng hoặc link file.${NC}"
        sleep 2
    fi
}

# ===== SET STATIC IP (UBUNTU NETPLAN) =====
set_static_ip() {
    echo -e "\n${CYAN}--- CẤU HÌNH IP TĨNH (UBUNTU NETPLAN) ---${NC}"
    echo -e "${YELLOW}Cảnh báo: Sai cấu hình có thể gây mất kết nối mạng VPS. Bạn chỉ nên dùng nếu đang kết nối qua VNC/Console ảo của nhà cung cấp.${NC}"
    
    # Liệt kê card mạng
    echo -e "\nDanh sách card mạng trên máy:"
    ip -br link | awk '$1 != "lo" {print "- " $1 " (" $3 ")"}'
    
    echo ""
    read -p "Nhập tên card mạng cần cài (VD: eth0, enp3s0): " interface
    [[ -z "$interface" ]] && return
    
    read -p "Nhập IP tĩnh kèm Subnet (VD: 192.168.1.100/24): " ip_addr
    [[ -z "$ip_addr" ]] && return
    
    read -p "Nhập Gateway (Default Router) (VD: 192.168.1.1): " gateway
    [[ -z "$gateway" ]] && return
    
    read -p "Nhập DNS Servers (Cách nhau bởi dấu phẩy, VD: 8.8.8.8,1.1.1.1): " dns_servers
    [[ -z "$dns_servers" ]] && dns_servers="8.8.8.8,1.1.1.1"
    
    # Chuyển đổi định dạng DNS cho netplan: "8.8.8.8,1.1.1.1" -> "8.8.8.8", "1.1.1.1"
    formatted_dns=$(echo "$dns_servers" | sed 's/,/","/g')
    formatted_dns="\"$formatted_dns\""
    
    echo -e "\n${YELLOW}Đang tạo file cấu hình giả lập (Netplan): /etc/netplan/01-static-ip.yaml${NC}"
    
    cat > /tmp/01-static-ip.yaml <<EOF
network:
  version: 2
  renderer: networkd
  ethernets:
    $interface:
      dhcp4: no
      addresses:
        - $ip_addr
      routes:
        - to: default
          via: $gateway
      nameservers:
        addresses: [$formatted_dns]
EOF
    
    echo -e "${GREEN}Bản xem trước cấu hình IP Tĩnh (Netplan):${NC}"
    cat /tmp/01-static-ip.yaml
    
    echo ""
    read -p "Bạn có chắc chắn muốn áp dụng thay đổi này không? (y/N): " confirm
    if [[ "$confirm" =~ ^[Yy]$ ]]; then
        mkdir -p /etc/netplan
        # Sao lưu cấu hình cũ (nếu có)
        mv /etc/netplan/*.yaml /etc/netplan/backup_old_netplan_$(date +%s).yaml.bak 2>/dev/null || true
        # Chép file cấu hình mới
        cp /tmp/01-static-ip.yaml /etc/netplan/01-static-ip.yaml
        chmod 600 /etc/netplan/01-static-ip.yaml
        rm -f /tmp/01-static-ip.yaml
        
        echo -e "${YELLOW}Đang áp dụng IP tĩnh (netplan apply)...${NC}"
        netplan apply
        echo -e "${GREEN}✔ Đã cấu hình IP tĩnh thành công! Nếu kết nối bị rớt, vui lòng ssh lại bằng IP mới: ${ip_addr%/*}${NC}"
    else
        rm -f /tmp/01-static-ip.yaml
        echo -e "${YELLOW}Đã hủy thao tác áp dụng cấu hình.${NC}"
    fi
}

# ===== QUẢN LÝ USER (UBUNTU) =====
manage_users() {
    while true; do
        clear
        echo -e "${CYAN}=== QUẢN LÝ USER UBUNTU ===${NC}"
        echo -e "1. Xem danh sách User hiện có"
        echo -e "2. Thêm User mới"
        echo -e "3. Xóa User"
        echo -e "4. Cấp quyền Quản trị (Sudo) cho User"
        echo -e "5. Đổi mật khẩu User"
        echo -e "0. Quay lại Menu chính"
        
        read -p "Chọn chức năng: " uc
        case $uc in
            1)
                echo -e "\n${GREEN}=> Danh sách User (có thư mục home, không tính nobody):${NC}"
                awk -F: '$3>=1000 && $1!="nobody" {print "- " $1}' /etc/passwd
                echo ""
                read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục..."
                ;;
            2)
                echo -e "\n${CYAN}--- THÊM USER MỚI ---${NC}"
                read -p "Nhập tên User muốn tạo: " new_user
                if [[ -n "$new_user" ]]; then
                    adduser "$new_user"
                fi
                echo ""
                read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục..."
                ;;
            3)
                echo -e "\n${RED}--- XÓA USER ---${NC}"
                read -p "Nhập tên User muốn xóa: " del_user
                if [[ -n "$del_user" ]]; then
                    read -p "Bạn có muốn xóa luôn thư mục Home của user này không? (y/N): " rm_home
                    if [[ "$rm_home" =~ ^[Yy]$ ]]; then
                        deluser --remove-home "$del_user"
                    else
                        deluser "$del_user"
                    fi
                fi
                echo ""
                read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục..."
                ;;
            4)
                echo -e "\n${YELLOW}--- CẤP QUYỀN SUDO ---${NC}"
                read -p "Nhập tên User muốn cấp quyền Sudo: " sudo_user
                if [[ -n "$sudo_user" ]]; then
                    usermod -aG sudo "$sudo_user"
                    echo -e "${GREEN}✔ Đã cấp quyền Sudo cho $sudo_user${NC}"
                fi
                echo ""
                read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục..."
                ;;
            5)
                echo -e "\n${YELLOW}--- ĐỔI MẬT KHẨU USER ---${NC}"
                read -p "Nhập tên User muốn đổi mật khẩu: " pwd_user
                if [[ -n "$pwd_user" ]]; then
                    passwd "$pwd_user"
                fi
                echo ""
                read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục..."
                ;;
            0) break ;;
            *) echo -e "${RED}Lựa chọn không hợp lệ!${NC}"; sleep 1 ;;
        esac
    done
}

# ===== SYSTEM TOOLS MENU =====
system_tools_menu() {
    while true; do
        clear
        echo -e "${CYAN}====================================================${NC}"
        echo -e "${YELLOW}                 🛠 CÔNG CỤ HỆ THỐNG${NC}"
        echo -e "${CYAN}====================================================${NC}"
        echo -e "   ${YELLOW}1.${NC} Cài đặt IP Tĩnh (Netplan) cho Ubuntu"
        echo -e "   ${YELLOW}2.${NC} Quản lý User máy (Thêm/Xóa/Sudo/Password)"
        echo -e "${CYAN}----------------------------------------------------${NC}"
        echo -e "   ${YELLOW}0.${NC} Quay lại Menu chính"
        echo -e "${CYAN}====================================================${NC}"
        read -p " ➔ Nhập lựa chọn của bạn: " sc
        case $sc in
            1) set_static_ip ;;
            2) manage_users ;;
            0) break ;;
            *) echo -e "${RED}❌ Lựa chọn không hợp lệ, vui lòng thử lại!${NC}"; sleep 1.5 ;;
        esac
    done
}

main_menu() {
    while true; do
        echo -e "\n${CYAN}========== TOOL QUẢN LÝ VPS - PHIÊN BẢN 0.0.6 / 04-04-2026 ==========${NC}"
        echo "1. Cài đặt hệ thống (FRP + Caddy + Web + Bảo mật)"
        echo "2. Quản lý Domain (Cho On-Demand TLS)"
        echo "3. Quản lý Port (UFW Firewall)"
        echo "4. Cập nhật trang chào mừng (Trang chủ truy cập qua IP)"
        echo "5. Cập nhật trang lỗi 404 (Khi thiết bị Client Offline)"
        echo "6. Xem trạng thái dịch vụ"
        echo "7. Xem thông tin VPS"
        echo "8. Restart dịch vụ"
        echo "9. Cập nhật Tool quản lý (Tải bản mới nhất)"
        echo "10. Công cụ Hệ thống (Set IP tĩnh, Quản lý User)"
        echo "0. Thoát"
        read -p "Nhập lựa chọn: " opt
        
        case $opt in
            1)
                echo -e "\n${BLUE}>>> Đang lấy thông tin khởi tạo...${NC}"
                B_PORT=$(get_free_port)
                V_PORT=$(get_free_port)
                API_PORT=$(get_free_port)
                TOKEN=$(openssl rand -hex 12)
                SEC_TOKEN=$(openssl rand -hex 16)
                show_install_info
                
                read -p "Bạn có muốn tiếp tục cài đặt không? (y/n): " confirm
                if [[ "$confirm" =~ ^[Yy]$ ]]; then
                    update_system
                    setup_swap
                    install_frp
                    install_caddy
                    security_harden
                    start_services
                    echo -e "\n${GREEN}✅ Cài đặt hệ thống hoàn tất!${NC}"
                else
                    echo -e "${YELLOW}Đã hủy cài đặt.${NC}"
                fi
                ;;
            2) manage_domains ;;
            3) manage_ports ;;
            4) update_homepage ;;
            5) update_404page ;;
            6) check_status ;;
            7) [ -f "$INFO_FILE" ] && cat "$INFO_FILE" || echo -e "${RED}Chưa có thông tin.${NC}" ;;
            8) systemctl restart frps caddy caddy-ask; check_status ;;
            9) update_script ;;
            10) system_tools_menu ;;
            0) echo -e "${GREEN}Tạm biệt!${NC}"; exit 0 ;;
            *) echo -e "${RED}Lựa chọn không hợp lệ!${NC}" ;;
        esac
    done
}

init_files
main_menu
END_OF_SCRIPT

# ================== 3. THIẾT LẬP ALIAS & CHẠY MENU ==================
sudo chmod +x /usr/local/bin/na

echo 'alias na="sudo /usr/local/bin/na"' | sudo tee /etc/profile.d/na-alias.sh >/dev/null
sudo chmod +x /etc/profile.d/na-alias.sh
if ! grep -q "alias na=" ~/.bashrc; then
    echo 'alias na="sudo /usr/local/bin/na"' >> ~/.bashrc
fi
eval 'alias na="sudo /usr/local/bin/na"'

rm -f ./vps_setup.sh ./na_setup.sh 2>/dev/null || true
clear

echo -e "\033[0;32m=======================================\033[0m"
echo -e "\033[0;32m     CÀI ĐẶT TOOL HOÀN TẤT THÀNH CÔNG!     \033[0m"
echo -e "\033[0;32m=======================================\033[0m"
echo -e "\033[0;36mĐang tự động khởi chạy tool...\033[0m"
sleep 2

na