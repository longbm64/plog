#!/bin/bash

# Màu sắc
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

APP_VERSION="v1.0.0-LITE"
BUILD_DATE="2026-08-07"

# Yêu cầu chạy quyền root
if [ "$EUID" -ne 0 ]; then
  echo -e "${YELLOW}He thong yeu cau quyen root de cai dat Systemd va FRPC. Vui long nhap mat khau sudo...${NC}"
  exec sudo "$0" "$@"
fi

get_frpc_status() {
    if command -v systemctl &> /dev/null && systemctl is-active --quiet frpc; then
        echo -e "${GREEN}DANG CHAY (ACTIVE - Systemd)${NC}"
    elif command -v launchctl &> /dev/null && launchctl list | grep -q com.nalink.frpc; then
        echo -e "${GREEN}DANG CHAY (ACTIVE - Launchd)${NC}"
    else
        echo -e "${RED}DANG DUNG / CHUA CAI DAT${NC}"
    fi
}

header() {
    clear
    echo -e "${CYAN}"
    echo '    _  __ ___    __    _         __   '
    echo '   / |/ // _ |  / /   (_)  ___  / /__ '
    echo '  /    // __ | / /__ / /  / _ \/  '"'"'_/ '
    echo ' /_/|_//_/ |_|/____//_/  /_//_/_/\_\  '
    printf "${RED}   [ CLIENT - LITE ]${NC}%27s\n" "${APP_VERSION} - ${BUILD_DATE}"
    echo ""
    echo -e "   Trạng thái FRPC: $(get_frpc_status)"
    echo ""
}

install_frpc() {
    if ! command -v frpc &> /dev/null; then
        echo -e "${YELLOW}>>> Dang tai xuong va cai dat FRPC...${NC}"
        FRP_VERSION="0.54.0"
        ARCH=$(uname -m)
        OS=$(uname -s)
        
        if [[ "$OS" == "Darwin" ]]; then
            FRP_OS="darwin"
        else
            FRP_OS="linux"
        fi

        if [[ "$ARCH" == "x86_64" ]]; then
            FRP_ARCH="amd64"
        elif [[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]]; then
            FRP_ARCH="arm64"
        else
            echo -e "${RED}Kien truc $ARCH khong duoc ho tro!${NC}"
            return 1
        fi
        
        curl -sL -o /tmp/frp.tar.gz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_${FRP_OS}_${FRP_ARCH}.tar.gz"
        tar -xzf /tmp/frp.tar.gz -C /tmp
        mv /tmp/frp_${FRP_VERSION}_${FRP_OS}_${FRP_ARCH}/frpc /usr/local/bin/
        chmod +x /usr/local/bin/frpc
        rm -rf /tmp/frp.tar.gz /tmp/frp_${FRP_VERSION}_${FRP_OS}_${FRP_ARCH}
        echo -e "${GREEN}✔ Cai dat FRPC thanh cong!${NC}"
    fi
}

connect_frps() {
    echo -e "${CYAN}--- KET NOI DEN SERVER FRPS ---${NC}"
    
    read -p "   Nhap IP cua Server FRPS: " SERVER_IP
    if [[ -z "$SERVER_IP" ]]; then
        echo -e "${RED}IP khong duoc de trong!${NC}"
        return
    fi
    
    read -p "   Nhap Port cua API Daemon (Enter de la 7400): " API_PORT
    API_PORT=${API_PORT:-7400}

    read -p "   Nhap Mat khau API (co the copy tren server): " API_PASS
    if [[ -z "$API_PASS" ]]; then
        echo -e "${RED}Mat khau khong duoc de trong!${NC}"
        return
    fi

    echo -e "\n${YELLOW}>>> Dang ket noi de lay thong tin...${NC}"
    
    # Goi API lay thong tin
    RESPONSE=$(curl -s -X POST "http://${SERVER_IP}:${API_PORT}/api/connection-info" \
        -H "Content-Type: application/json" \
        -d "{\"password\": \"${API_PASS}\"}")

    # Parse JSON bang grep/awk don gian de tranh phu thuoc jq
    BIND_PORT=$(echo "$RESPONSE" | grep -o '"bind_port":"[^"]*' | grep -o '[^"]*$')
    TOKEN=$(echo "$RESPONSE" | grep -o '"token":"[^"]*' | grep -o '[^"]*$')

    if [[ -z "$BIND_PORT" || -z "$TOKEN" ]]; then
        echo -e "${RED}❌ Ket noi that bai! Sai IP, Port hoac Mat khau.${NC}"
        echo -e "Phan hoi tu server: $RESPONSE"
        return
    fi

    echo -e "${GREEN}✔ Lay thong tin thanh cong! Bind Port: $BIND_PORT${NC}"

    install_frpc

    # Tao file cau hinh frpc.toml
    echo -e "${YELLOW}>>> Dang cau hinh va khoi dong FRPC...${NC}"
    
    mkdir -p /etc/frp
    cat << EOF > /etc/frp/frpc.toml
serverAddr = "${SERVER_IP}"
serverPort = ${BIND_PORT}

auth.method = "token"
auth.token = "${TOKEN}"

    echo -e "\n${CYAN}--- CAU HINH WEBSITE (PROXY) ---${NC}"
    read -p "   Ban co muon tao Proxy cho Website khong? (y/n) [y]: " ADD_PROXY
    ADD_PROXY=${ADD_PROXY:-y}

    if [[ "$ADD_PROXY" == "y" || "$ADD_PROXY" == "Y" ]]; then
        read -p "   Nhap Domain hoac Subdomain (VD: a.test.nalink.app): " PROXY_DOMAIN
        read -p "   Nhap Port Local cua ung dung web dang chay (VD: 3000): " PROXY_PORT
        
        cat << EOF >> /etc/frp/frpc.toml

[[proxies]]
name = "web-proxy-${PROXY_PORT}"
type = "http"
localPort = ${PROXY_PORT}
customDomains = ["${PROXY_DOMAIN}"]
EOF
        echo -e "${GREEN}✔ Da them cau hinh Proxy cho domain: $PROXY_DOMAIN (Port: $PROXY_PORT)${NC}"
    else
        echo -e "${YELLOW}* Ban co the them cau hinh Proxy vao /etc/frp/frpc.toml sau.${NC}"
    fi

    # Tao Systemd service
    if [[ "$OS" == "Darwin" ]]; then
        # MacOS Launchd
        cat << 'EOF' > /Library/LaunchDaemons/com.nalink.frpc.plist
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.nalink.frpc</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/frpc</string>
        <string>-c</string>
        <string>/etc/frp/frpc.toml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
EOF
        launchctl unload /Library/LaunchDaemons/com.nalink.frpc.plist 2>/dev/null
        launchctl load /Library/LaunchDaemons/com.nalink.frpc.plist
    else
        # Linux Systemd
        cat << 'EOF' > /etc/systemd/system/frpc.service
[Unit]
Description=Frp Client Service
After=network.target

[Service]
Type=simple
User=root
Restart=on-failure
RestartSec=5s
ExecStart=/usr/local/bin/frpc -c /etc/frp/frpc.toml

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable frpc >/dev/null 2>&1
        systemctl restart frpc
    fi

    echo -e "${GREEN}✅ Da ket noi voi Server FRPS thanh cong!${NC}"
}

show_menu() {
    while true; do
        header
        echo -e "${YELLOW}   ========== MENU CLIENT ==========${NC}"
        echo "   1. Ket noi FRPS (Lay cau hinh tu dong tu Server)"
        echo "   0. Thoat"
        echo ""
        read -p "   🔹 Xin moi chon (0-1): " choice

        case $choice in
            1)
                connect_frps
                ;;
            0)
                echo "   👋 Tam biet!"
                exit 0
                ;;
            *)
                echo -e "${RED}❌ Lua chon khong hop le!${NC}"
                ;;
        esac
        echo ""
        read -p "(Nhan Enter de tiep tuc...)" dummy
    done
}

show_menu
