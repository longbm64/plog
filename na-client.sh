#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRPC_CONF="$SCRIPT_DIR/frpc.toml"
PM2_NAME="frpc"

GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

[ ! -f "$FRPC_CONF" ] && touch "$FRPC_CONF"

# ===== ĐẢM BẢO [webServer] LUÔN CÓ TRONG CONFIG =====
# Phải gọi TRƯỚC khi frpc khởi động, để admin API sẵn sàng ngay từ đầu
ensure_webserver_config() {
    if grep -q "\[webServer\]" "$FRPC_CONF"; then
        return 0  # Đã có rồi
    fi
    # Chèn [webServer] trước [[proxies]] đầu tiên, hoặc cuối file nếu chưa có proxy
    awk '
    BEGIN { done = 0 }
    /^\[\[proxies\]\]/ && !done {
        print "\n[webServer]"
        print "addr = \"127.0.0.1\""
        print "port = 7400\n"
        done = 1
    }
    { print $0 }
    END {
        if (!done) {
            print "\n[webServer]"
            print "addr = \"127.0.0.1\""
            print "port = 7400"
        }
    }' "$FRPC_CONF" > "${FRPC_CONF}.tmp" && mv "${FRPC_CONF}.tmp" "$FRPC_CONF"
    return 1  # Vừa thêm mới
}

# ===== DỌN DẸP PROXIES RỖNG =====
cleanup_empty_proxies() {
    awk '
    BEGIN { buffer = ""; is_proxy = 0; is_empty = 0 }
    /^\[\[proxies\]\]/ {
        if (is_proxy) {
            if (!is_empty) print buffer;
        } else {
            if (buffer != "") print buffer;
        }
        buffer = $0;
        is_proxy = 1;
        is_empty = 0;
        next;
    }
    {
        if (is_proxy) {
            buffer = buffer "\n" $0;
            if ($0 ~ /customDomains/) {
                val = $0;
                sub(/.*\[/, "", val);
                sub(/\].*/, "", val);
                gsub(/[" \t,]/, "", val);
                if (val == "") is_empty = 1;
            }
        } else {
            if (buffer == "") buffer = $0;
            else buffer = buffer "\n" $0;
        }
    }
    END {
        if (is_proxy) {
            if (!is_empty) print buffer;
        } else {
            if (buffer != "") print buffer;
        }
    }
    ' "$FRPC_CONF" > "${FRPC_CONF}.tmp" && mv "${FRPC_CONF}.tmp" "$FRPC_CONF"
}

# ===== RELOAD FRPC - DÙNG ADMIN API (KHÔNG NGẮT SSH) =====
# Gọi trực tiếp HTTP API trên 127.0.0.1:7400 thay vì dùng CLI frpc reload
# Điều này đảm bảo chỉ cập nhật cấu hình proxy mà KHÔNG restart tiến trình frpc
reload_frpc() {
    if ! pm2 list 2>/dev/null | grep -q "$PM2_NAME"; then
        echo -e "${YELLOW}⚠ FRPC chưa chạy qua PM2, bỏ qua reload.${NC}"
        return
    fi

    # Đảm bảo config có [webServer] (nếu chưa có thì thêm vào file,
    # nhưng KHÔNG restart - chỉ ghi chú cho lần khởi động tiếp theo)
    if ! grep -q "\[webServer\]" "$FRPC_CONF"; then
        ensure_webserver_config
        echo -e "${YELLOW}⚠ Admin API (webServer) chưa được kích hoạt trong phiên chạy hiện tại.${NC}"
        echo -e "${YELLOW}  Cấu hình đã được lưu vào file. Thay đổi sẽ có hiệu lực khi:${NC}"
        echo -e "   • Thoát SSH → Chọn mục ${CYAN}6${NC} để khởi chạy lại FRPC"
        echo -e "   • Hoặc chạy trực tiếp trên máy (không qua FRP SSH)"
        return
    fi

    echo -e "${CYAN}Đang hot-reload cấu hình (không ngắt SSH)...${NC}"

    # Gọi Admin API trực tiếp bằng curl (an toàn hơn CLI frpc reload)
    RELOAD_OUT=$(curl -s -o /dev/null -w "%{http_code}" -X GET "http://127.0.0.1:7400/api/reload" 2>&1)

    if [ "$RELOAD_OUT" = "200" ]; then
        echo -e "${GREEN}✔ Hot-Reload thành công! Kết nối SSH/Proxy cũ vẫn giữ nguyên.${NC}"
    elif [ "$RELOAD_OUT" = "000" ]; then
        # Không kết nối được đến admin API → frpc đang chạy nhưng không có webServer
        echo -e "${YELLOW}⚠ Admin API chưa sẵn sàng (port 7400 chưa mở).${NC}"
        echo -e "${YELLOW}  Cấu hình đã được lưu vào file. Để áp dụng:${NC}"
        echo -e "   • Thoát SSH → Chọn mục ${CYAN}6${NC} để khởi chạy lại FRPC"
    else
        echo -e "${RED}⚠ Hot-Reload trả về HTTP $RELOAD_OUT${NC}"
        echo -e "${YELLOW}Khắc phục:${NC}"
        echo -e "   • Kiểm tra config: ${CYAN}frpc verify -c \"$FRPC_CONF\"${NC}"
        echo -e "   • Thoát SSH → Chọn mục ${CYAN}6${NC} để khởi chạy lại FRPC"
    fi
}

# ===== TEST CONNECTION =====
test_connection() {
    echo -e "\n${CYAN}>>> Đang kiểm tra kết nối đến Server...${NC}"

    SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
    SERVER_PORT=$(grep -E "^serverPort" "$FRPC_CONF" | awk '{print $3}')

    if [[ -z "$SERVER_IP" || -z "$SERVER_PORT" ]]; then
        echo -e "${RED}❌ Chưa có thông tin IP/Port. Vui lòng cấu hình trước (Mục 5).${NC}"
        return
    fi

    echo -e "Kiểm tra → IP: ${YELLOW}$SERVER_IP${NC} | Port: ${YELLOW}$SERVER_PORT${NC}"

    if [[ "$OSTYPE" == "darwin"* ]]; then
        NC_CMD="nc -z -G 4"
    else
        NC_CMD="nc -z -w 4"
    fi

    if $NC_CMD "$SERVER_IP" "$SERVER_PORT" 2>/dev/null || bash -c "cat < /dev/null > /dev/tcp/$SERVER_IP/$SERVER_PORT" 2>/dev/null; then
        echo -e "${GREEN}✔ Kết nối TCP thành công!${NC}"

        if pm2 list 2>/dev/null | grep -q "$PM2_NAME"; then
            sleep 2
            if pm2 logs "$PM2_NAME" --lines 30 --nostream 2>/dev/null | grep -qi "login to server success"; then
                echo -e "${GREEN}✔ Token hợp lệ - FRPC đang kết nối tốt!${NC}"
            elif pm2 logs "$PM2_NAME" --lines 30 --nostream 2>/dev/null | grep -qi "authorization failed"; then
                echo -e "${RED}❌ Sai Token!${NC}"
            else
                echo -e "${CYAN}ℹ PM2 đang chạy.${NC}"
            fi
        fi
    else
        echo -e "${RED}❌ Không thể kết nối đến Server!${NC}"
        echo -e "💡 Kiểm tra lại IP hoặc mở port $SERVER_PORT trên VPS.${NC}"
    fi
}

# ===== LIST DOMAINS =====
list_domains() {
    echo -e "\n${CYAN}========= DANH SÁCH PROXY ĐANG CHẠY =========${NC}"
    
    if ! grep -q "\[\[proxies\]\]" "$FRPC_CONF"; then
        echo -e "${YELLOW}Hiện tại chưa có proxy nào.${NC}"
        return
    fi

    awk '
    BEGIN { port=""; type=""; remote=""; domains="" }
    /^\[\[proxies\]\]/ {
        if (port != "") {
            if (type == "\"http\"") {
                printf("\033[0;32m▶ [HTTP] Local Port %s:\033[0m\n", port)
                n=split(domains, arr, ",")
                for (i=1;i<=n;i++) {
                    gsub(/[" ]/, "", arr[i])
                    if (arr[i] != "") printf("   - %s\n", arr[i])
                }
            } else if (type == "\"tcp\"") {
                printf("\033[0;34m▶ [TCP] Local Port %s ➔ FRP Port %s\033[0m\n", port, remote)
            }
        }
        port=""; type=""; remote=""; domains=""
    }
    /type =/ { type=$3 }
    /localPort =/ { port=$3; gsub(/[^0-9]/,"",port) }
    /remotePort =/ { remote=$3; gsub(/[^0-9]/,"",remote) }
    /customDomains =/ {
        line=$0; sub(/.*\[/,"",line); sub(/\].*/,"",line); domains=line
    }
    END {
        if (port != "") {
            if (type == "\"http\"") {
                printf("\033[0;32m▶ [HTTP] Local Port %s:\033[0m\n", port)
                n=split(domains, arr, ",")
                for (i=1;i<=n;i++) {
                    gsub(/[" ]/, "", arr[i])
                    if (arr[i] != "") printf("   - %s\n", arr[i])
                }
            } else if (type == "\"tcp\"") {
                printf("\033[0;34m▶ [TCP] Local Port %s ➔ FRP Port %s\033[0m\n", port, remote)
            }
        }
    }
    ' "$FRPC_CONF"
}

# ===== ADD DOMAIN =====
add_domain() {
    while true; do
        clear
        list_domains
        echo -e "\n${CYAN}--- THÊM PROXY / DOMAIN MỚI ---${NC}"
        echo -e "${YELLOW}(Để trống + Enter để quay lại Menu chính)${NC}"
        echo -e "1. HTTP (Dành cho Web / Domain)"
        echo -e "2. TCP  (Dành cho SSH / Database / Game...)"

        read -p "Chọn loại kết nối (1 hoặc 2): " p_type
        [[ -z "$p_type" ]] && break

        if [[ "$p_type" == "2" ]]; then
            read -p "Nhập Port local (VD: 22): " port
            [[ -z "$port" ]] && continue
            read -p "Nhập Remote Port (Enter = random): " remote_port

            if [[ -z "$remote_port" ]]; then
                while true; do
                    remote_port=$((RANDOM % 40000 + 10000))
                    if ! grep -q "remotePort = $remote_port" "$FRPC_CONF"; then
                        break
                    fi
                done
                echo -e "${YELLOW}Đã tạo ngẫu nhiên Remote Port: ${CYAN}$remote_port${NC}"
            elif grep -q "remotePort = $remote_port" "$FRPC_CONF"; then
                echo -e "${RED}❌ Remote Port [$remote_port] đã tồn tại!${NC}"
                read -n 1 -s -r
                continue
            fi

            cat >> "$FRPC_CONF" <<EOL

[[proxies]]
name = "tcp_${port}_${remote_port}"
type = "tcp"
localIP = "127.0.0.1"
localPort = $port
remotePort = $remote_port
EOL
            echo -e "${GREEN}✔ Đã thêm Proxy TCP (Local: $port → FRP: $remote_port)${NC}"

            # Gọi API mở port
            SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
            API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
            API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
            if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
                echo -e "${YELLOW}Đang yêu cầu Server mở port qua API...${NC}"
                curl -s -X POST -d "token=$API_TOKEN&port=$remote_port&action=add_port" "http://$SERVER_IP:$API_PORT"
            fi

            reload_frpc

        elif [[ "$p_type" == "1" ]]; then
            read -p "Nhập Domain: " domain
            [[ -z "$domain" ]] && continue
            
            if grep -q "\"$domain\"" "$FRPC_CONF"; then
                echo -e "${RED}❌ Domain [$domain] đã tồn tại!${NC}"
                read -n 1 -s -r
                continue
            fi
            
            read -p "Nhập Port local: " port
            [[ -z "$port" ]] && continue

            if grep -q "localPort = $port" "$FRPC_CONF" && grep -q "type = \"http\"" "$FRPC_CONF"; then
                if [[ "$OSTYPE" == "darwin"* ]]; then
                    sed -i '' "/localPort = $port/,/customDomains/ s/\]/,\"$domain\"\]/" "$FRPC_CONF"
                else
                    sed -i "/localPort = $port/,/customDomains/ s/\]/,\"$domain\"\]/" "$FRPC_CONF"
                fi
            else
                cat >> "$FRPC_CONF" <<EOL

[[proxies]]
name = "proxy_$port"
type = "http"
localIP = "127.0.0.1"
localPort = $port
customDomains = ["$domain"]
EOL
            fi

            echo -e "${GREEN}✔ Đã thêm domain [$domain] (port $port)${NC}"

            # Gọi API thêm domain
            SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
            API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
            API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
            if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
                echo -e "${YELLOW}Đang đồng bộ domain lên Server...${NC}"
                curl -s -X POST -d "token=$API_TOKEN&domain=$domain" "http://$SERVER_IP:$API_PORT"
            fi
            reload_frpc
        else
            echo -e "${RED}❌ Lựa chọn không hợp lệ!${NC}"
        fi
        
        echo -e "\n${CYAN}Nhấn phím bất kỳ để tiếp tục thêm...${NC}"
        read -n 1 -s -r
    done
}

# ===== DELETE DOMAIN =====
delete_domain() {
    while true; do
        clear
        list_domains
        echo -e "\n${CYAN}--- XÓA PROXY / DOMAIN ---${NC}"
        echo -e "${YELLOW}Nhập Domain hoặc Port local cần xóa (Enter để thoát)${NC}"
        
        read -p "Nhập: " input
        [[ -z "$input" ]] && break

        if [[ "$input" =~ ^[0-9]+$ ]]; then
            # Xóa theo localPort (TCP)
            remote_port_to_close=$(awk -v p="$input" '
            BEGIN {found=0; rp=""}
            /^\[\[proxies\]\]/ {
                if (found==1 && rp!="") print rp;
                found=0; rp=""
            }
            $1=="localPort" && $3==p {found=1}
            $1=="remotePort" {rp=$3}
            END {if (found==1 && rp!="") print rp}
            ' "$FRPC_CONF" | tr -d '\r' | head -n 1)

            awk -v p="$input" '
            BEGIN { skip=0; buffer="" }
            /^\[\[proxies\]\]/ {
                if (buffer != "") { if (skip == 0) print buffer; }
                buffer = $0; skip = 0; next;
            }
            {
                if (buffer == "") buffer = $0;
                else buffer = buffer "\n" $0;
                if ($1 == "localPort" && $3 == p) skip = 1;
            }
            END { if (buffer != "" && skip == 0) print buffer; }
            ' "$FRPC_CONF" > "${FRPC_CONF}.tmp" && mv "${FRPC_CONF}.tmp" "$FRPC_CONF"
            
            echo -e "${GREEN}✔ Đã xóa proxy port local [$input].${NC}"

            if [[ -n "$remote_port_to_close" ]]; then
                SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
                API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
                API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
                if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
                    curl -s -X POST -d "token=$API_TOKEN&port=$remote_port_to_close&action=delete_port" "http://$SERVER_IP:$API_PORT"
                fi
            fi
        else
            # Xóa domain HTTP
            if [[ "$OSTYPE" == "darwin"* ]]; then
                sed -i '' "s/\"$input\",//g; s/,\"$input\"//g; s/\"$input\"//g" "$FRPC_CONF"
            else
                sed -i "s/\"$input\",//g; s/,\"$input\"//g; s/\"$input\"//g" "$FRPC_CONF"
            fi
            cleanup_empty_proxies
            echo -e "${GREEN}✔ Đã xóa domain [$input] và dọn dẹp.${NC}"

            SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
            API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
            API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
            if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
                curl -s -X POST -d "token=$API_TOKEN&domain=$input&action=delete" "http://$SERVER_IP:$API_PORT"
            fi
        fi

        reload_frpc
        echo -e "\n${CYAN}Nhấn phím bất kỳ để tiếp tục xóa...${NC}"
        read -n 1 -s -r
    done
}

# ===== CHANGE PORT =====
change_port() {
    while true; do
        clear
        list_domains
        echo -e "\n${CYAN}--- ĐỔI PORT CHO DOMAIN (HTTP) ---${NC}"
        echo -e "${YELLOW}(Để trống + Enter để quay lại)${NC}"
        
        read -p "Nhập Domain cần đổi: " domain
        [[ -z "$domain" ]] && break
        read -p "Nhập Port mới: " new_port
        [[ -z "$new_port" ]] && break

        if ! grep -q "$domain" "$FRPC_CONF"; then
            echo -e "${RED}❌ Domain không tồn tại!${NC}"
            sleep 1.5
            continue
        fi

        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' "s/\"$domain\",//g; s/,\"$domain\"//g; s/\"$domain\"//g" "$FRPC_CONF"
        else
            sed -i "s/\"$domain\",//g; s/,\"$domain\"//g; s/\"$domain\"//g" "$FRPC_CONF"
        fi
        cleanup_empty_proxies

        if grep -q "localPort = $new_port" "$FRPC_CONF"; then
            if [[ "$OSTYPE" == "darwin"* ]]; then
                sed -i '' "/localPort = $new_port/,/customDomains/ s/\]/,\"$domain\"\]/" "$FRPC_CONF"
            else
                sed -i "/localPort = $new_port/,/customDomains/ s/\]/,\"$domain\"\]/" "$FRPC_CONF"
            fi
        else
            cat >> "$FRPC_CONF" <<EOL

[[proxies]]
name = "proxy_$new_port"
type = "http"
localIP = "127.0.0.1"
localPort = $new_port
customDomains = ["$domain"]
EOL
        fi

        echo -e "${GREEN}✔ Đã chuyển domain [$domain] sang port [$new_port]${NC}"
        reload_frpc
        
        echo -e "\n${CYAN}Nhấn phím bất kỳ để tiếp tục...${NC}"
        read -n 1 -s -r
    done
}

# ===== CONFIG SERVER (ĐÃ CẢI THIỆN) =====
config_server() {
    echo -e "\n${CYAN}--- CẤU HÌNH KẾT NỐI SERVER ---${NC}"
    
    cur_ip=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
    cur_port=$(grep -E "^serverPort" "$FRPC_CONF" | awk '{print $3}')
    cur_token=$(grep -E "^auth\.token" "$FRPC_CONF" | cut -d'"' -f2)
    cur_api_port=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
    cur_api_token=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)

    echo -e "${YELLOW}Thông tin hiện tại:${NC}"
    echo -e " - Server IP        : ${GREEN}${cur_ip:-[Trống]}${NC}"
    echo -e " - FRP Bind Port    : ${GREEN}${cur_port:-[Trống]}${NC}"
    echo -e " - Token            : ${GREEN}${cur_token:-[Trống]}${NC}"
    echo -e " - API Port         : ${GREEN}${cur_api_port:-[Trống]}${NC}"
    echo -e " - API Token        : ${GREEN}${cur_api_token:-[Trống]}${NC}"
    
    echo ""
    read -p "Bạn có muốn thay đổi? (y/N): " choice
    if [[ ! "$choice" =~ ^[Yy]$ ]]; then
        return
    fi

    echo -e "\n${CYAN}(Nhấn Enter để giữ nguyên giá trị cũ)${NC}"
    
    read -p "Server IP [$cur_ip]: " ip
    ip=${ip:-$cur_ip}
    
    read -p "FRP Bind Port [$cur_port]: " port
    port=${port:-$cur_port}
    
    read -p "Token [$cur_token]: " token
    token=${token:-$cur_token}
    
    read -p "API Port [$cur_api_port]: " api_port
    api_port=${api_port:-$cur_api_port}
    
    read -p "API Token [$cur_api_token]: " api_token
    api_token=${api_token:-$cur_api_token}

    # Sao lưu phần proxies
    sed -n '/\[\[proxies\]\]/,$p' "$FRPC_CONF" > /tmp/frpc_proxies.tmp 2>/dev/null || true

    cat > "$FRPC_CONF" <<EOL
# API_PORT=$api_port
# API_TOKEN=$api_token
serverAddr = "$ip"
serverPort = $port
auth.token = "$token"

[webServer]
addr = "127.0.0.1"
port = 7400
EOL

    if [ -s /tmp/frpc_proxies.tmp ]; then
        echo "" >> "$FRPC_CONF"
        cat /tmp/frpc_proxies.tmp >> "$FRPC_CONF"
    fi
    rm -f /tmp/frpc_proxies.tmp

    echo -e "${GREEN}✔ Cập nhật file cấu hình thành công!${NC}"
    
    if pm2 list 2>/dev/null | grep -q "$PM2_NAME"; then
        pm2 restart "$PM2_NAME" >/dev/null 2>&1
    fi
    
    test_connection
}

# ===== RUN FRPC =====
run_frpc() {
    if ! command -v frpc >/dev/null 2>&1; then
        echo -e "${RED}❌ Chưa cài đặt frpc. Vui lòng chọn mục 7.${NC}"
        return
    fi
    if ! command -v pm2 >/dev/null 2>&1; then
        echo -e "${RED}❌ Chưa cài đặt PM2. Vui lòng chọn mục 7.${NC}"
        return
    fi

    # ĐẢM BẢO [webServer] có trong config TRƯỚC KHI khởi động
    # Điều này giúp admin API (port 7400) sẵn sàng ngay khi frpc start
    # → hot-reload sẽ luôn hoạt động từ lần đầu tiên
    ensure_webserver_config

    echo -e "${YELLOW}Đang khởi động FRPC...${NC}"
    pm2 delete "$PM2_NAME" >/dev/null 2>&1
    pm2 start "$(which frpc)" --name "$PM2_NAME" -- -c "$FRPC_CONF" >/dev/null 2>&1
    pm2 save >/dev/null 2>&1

    sleep 2
    
    if pm2 list --no-color 2>/dev/null | grep -w "$PM2_NAME" | grep -qw "online"; then
        LAST_LOG=$(pm2 logs "$PM2_NAME" --lines 30 --nostream 2>/dev/null | grep -iE "success|fail|error|refused|already used|no such host|timeout" | tail -n 1)
        if echo "$LAST_LOG" | grep -qiE "fail|error|refused|already used|no such host|timeout"; then
            echo -e "${RED}❌ FRPC chạy nhưng kết nối thất bại!${NC}"
            echo -e "${YELLOW}Chi tiết: $LAST_LOG${NC}"
            pm2 stop "$PM2_NAME" >/dev/null 2>&1
        else
            echo -e "${GREEN}✔ FRPC đã chạy nền và kết nối thành công!${NC}"
            echo -e "${CYAN}ℹ Admin API (Hot-Reload) đang lắng nghe trên port 7400${NC}"
        fi
    else
        echo -e "${RED}❌ Không thể khởi chạy FRPC.${NC}"
        echo -e "${YELLOW}Chi tiết lỗi từ PM2:${NC}"
        pm2 logs "$PM2_NAME" --lines 20 --nostream 2>/dev/null | tail -n 15
    fi
}

# ===== INSTALL FRP =====
install_frp() {
    echo -e "\n${CYAN}--- CÀI ĐẶT FRPC TỰ ĐỘNG ---${NC}"

    if ! command -v pm2 >/dev/null 2>&1; then
        echo -e "${YELLOW}Đang cài Node.js và PM2...${NC}"
        if [[ "$(uname)" == "Linux" ]]; then
            curl -fsSL https://deb.nodesource.com/setup_current.x | sudo -E bash -
            sudo apt install -y nodejs
        elif [[ "$(uname)" == "Darwin" ]] && command -v brew >/dev/null; then
            brew install node
        else
            echo -e "${RED}Không hỗ trợ tự động cài Node.js trên hệ thống này.${NC}"
            return
        fi
        sudo npm install -g pm2
    fi

    LATEST_VERSION=$(curl -s https://api.github.com/repos/fatedier/frp/releases/latest | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
    [ -z "$LATEST_VERSION" ] && LATEST_VERSION="0.61.0"

    OS=$(uname)
    ARCH=$(uname -m)
    if [[ "$OS" == "Darwin" ]]; then
        FILE="frp_${LATEST_VERSION}_darwin_${ARCH/arm64/arm64}"
    else
        FILE="frp_${LATEST_VERSION}_linux_${ARCH/x86_64/amd64}"
    fi
    if [[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]]; then
        FILE="frp_${LATEST_VERSION}_${OS,,}_arm64"
    fi

    URL="https://github.com/fatedier/frp/releases/download/v${LATEST_VERSION}/${FILE}.tar.gz"

    echo -e "${YELLOW}Đang tải frpc v${LATEST_VERSION}...${NC}"
    wget -q --show-progress "$URL" || curl -L -O "$URL"

    tar -xzf "${FILE}.tar.gz"
    cd "$FILE" || exit
    sudo cp frpc /usr/local/bin/
    sudo chmod +x /usr/local/bin/frpc
    cd ..
    rm -rf "$FILE" "${FILE}.tar.gz"

    echo -e "${GREEN}✔ Cài đặt frpc thành công!${NC}"
}

# ===== UPDATE SCRIPT =====
update_script() {
    echo -e "\n${CYAN}--- CẬP NHẬT TOOL ---${NC}"
    UPDATE_URL="https://longbm64.github.io/plog/vps/na-client.sh"
    # Lấy đường dẫn chính xác của file đang chạy (kể cả qua sudo/alias)
    SELF_PATH="$(readlink -f "${BASH_SOURCE[0]}" 2>/dev/null || realpath "${BASH_SOURCE[0]}" 2>/dev/null || echo "${BASH_SOURCE[0]}")"

    CUR_VER=$(grep 'PHIÊN BẢN' "$SELF_PATH" 2>/dev/null | sed 's/.*PHIÊN BẢN //' | sed 's/".*//' | head -1)
    echo -e "${YELLOW}Phiên bản hiện tại: ${CYAN}$CUR_VER${NC}"
    echo -e "${YELLOW}Đang tải và ghi đè trực tiếp vào: ${CYAN}$SELF_PATH${NC}"

    # Tải thẳng vào RAM rồi ghi đè file gốc (không dùng /tmp)
    NEW_CONTENT=$(curl -s -f -L "$UPDATE_URL" 2>/dev/null)

    if [ -z "$NEW_CONTENT" ]; then
        echo -e "${RED}❌ Không thể tải bản cập nhật. Kiểm tra kết nối mạng.${NC}"
        return
    fi

    NEW_VER=$(echo "$NEW_CONTENT" | grep 'PHIÊN BẢN' | sed 's/.*PHIÊN BẢN //' | sed 's/".*//' | head -1)

    # Ghi đè trực tiếp
    echo "$NEW_CONTENT" > "$SELF_PATH"
    chmod +x "$SELF_PATH"

    echo -e "${GREEN}✔ Cập nhật thành công! ($CUR_VER → $NEW_VER)${NC}"
    echo -e "${GREEN}File đã ghi đè: $SELF_PATH${NC}"
    echo -e "${CYAN}Đang khởi chạy lại bản mới...${NC}"
    sleep 1

    exec bash "$SELF_PATH"
}

# ===== DOCKER: CÀI ĐẶT =====
install_docker() {
    echo -e "\n${CYAN}--- CÀI ĐẶT DOCKER ---${NC}"
    if command -v docker >/dev/null 2>&1; then
        DOCKER_VER=$(docker --version | sed 's/Docker version //' | cut -d',' -f1)
        echo -e "${GREEN}✔ Docker đã được cài đặt (v$DOCKER_VER)${NC}"
        echo ""
        read -p "Bạn có muốn cài đặt lại / cập nhật không? (y/N): " choice
        [[ ! "$choice" =~ ^[Yy]$ ]] && return
    fi

    echo -e "${YELLOW}Đang cài đặt Docker...${NC}"
    if [[ "$(uname)" == "Linux" ]]; then
        curl -fsSL https://get.docker.com | sh
        sudo systemctl enable docker
        sudo systemctl start docker
        # Thêm user hiện tại vào group docker (không cần sudo mỗi lần)
        REAL_USER="${SUDO_USER:-$USER}"
        sudo usermod -aG docker "$REAL_USER"
        echo -e "${GREEN}✔ Cài đặt Docker thành công!${NC}"
        echo -e "${YELLOW}ℹ User '$REAL_USER' đã được thêm vào group docker.${NC}"
        echo -e "${YELLOW}  Hãy logout và login lại để chạy docker không cần sudo.${NC}"
    elif [[ "$(uname)" == "Darwin" ]]; then
        echo -e "${YELLOW}Trên macOS, vui lòng tải Docker Desktop từ:${NC}"
        echo -e "${CYAN}https://www.docker.com/products/docker-desktop${NC}"
    else
        echo -e "${RED}❌ Hệ điều hành không được hỗ trợ tự động.${NC}"
    fi
}

# ===== DOCKER: DANH SÁCH CONTAINER =====
docker_list_containers() {
    echo -e "\n${CYAN}========= DANH SÁCH CONTAINER =========${NC}"
    if ! command -v docker >/dev/null 2>&1; then
        echo -e "${RED}❌ Docker chưa được cài đặt. Chọn mục 1 để cài.${NC}"
        return
    fi

    # Đếm container
    RUNNING=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
    ALL=$(docker ps -aq 2>/dev/null | wc -l | tr -d ' ')
    echo -e "${GREEN}Đang chạy: $RUNNING${NC} | ${YELLOW}Tổng cộng: $ALL${NC}\n"

    if [ "$ALL" -eq 0 ]; then
        echo -e "${YELLOW}Chưa có container nào.${NC}"
        return
    fi

    # Lấy Server IP từ config
    SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)

    # Hiển thị chi tiết từng container kèm thông tin tài khoản + proxy
    docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}' 2>/dev/null | while IFS='|' read -r cname cimage cstatus cports; do
        # Trạng thái màu
        if echo "$cstatus" | grep -qi "^Up"; then
            STATUS_ICON="${GREEN}🟢 UP${NC}"
        else
            STATUS_ICON="${RED}🔴 DOWN${NC}"
        fi

        echo -e "${CYAN}┌─────────────────────────────────────────${NC}"
        echo -e "${CYAN}│${NC} ${YELLOW}$cname${NC}  [$STATUS_ICON]"
        echo -e "${CYAN}│${NC}  Image: $cimage"

        # Trích xuất local ports từ docker và tìm proxy tương ứng trong frpc.toml
        # Format ports: "0.0.0.0:12345->3306/tcp" → lấy 12345
        LOCAL_PORTS=$(echo "$cports" | grep -oE '[0-9]+\->' | sed 's/->.*//' | sort -u)
        for lp in $LOCAL_PORTS; do
            # Tìm remotePort tương ứng trong frpc.toml
            REMOTE_P=$(awk -v p="$lp" '
                /^\[\[proxies\]\]/ { rp=""; found_lp=0 }
                $1=="localPort" && $3==p { found_lp=1 }
                $1=="remotePort" { rp=$3 }
                /^\[\[proxies\]\]/ || /^$/ {
                    if (found_lp && rp!="") print rp
                }
                END { if (found_lp && rp!="") print rp }
            ' "$FRPC_CONF" 2>/dev/null | head -1)

            if [ -n "$REMOTE_P" ] && [ -n "$SERVER_IP" ]; then
                echo -e "${CYAN}│${NC}  Port:  Local ${GREEN}$lp${NC} → FRP ${GREEN}${SERVER_IP}:${REMOTE_P}${NC}"
            else
                echo -e "${CYAN}│${NC}  Port:  Local ${GREEN}$lp${NC} ${YELLOW}(chưa có proxy)${NC}"
            fi
        done

        # Trích xuất thông tin tài khoản từ env vars của container
        ENVS=$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$cname" 2>/dev/null)
        CRED_USER="" CRED_PASS=""

        # MySQL
        MYSQL_PASS=$(echo "$ENVS" | grep '^MYSQL_ROOT_PASSWORD=' | cut -d'=' -f2-)
        [ -n "$MYSQL_PASS" ] && CRED_USER="root" && CRED_PASS="$MYSQL_PASS"

        # MongoDB
        MONGO_USER=$(echo "$ENVS" | grep '^MONGO_INITDB_ROOT_USERNAME=' | cut -d'=' -f2-)
        MONGO_PASS=$(echo "$ENVS" | grep '^MONGO_INITDB_ROOT_PASSWORD=' | cut -d'=' -f2-)
        [ -n "$MONGO_USER" ] && CRED_USER="$MONGO_USER" && CRED_PASS="$MONGO_PASS"

        # PostgreSQL
        PG_USER=$(echo "$ENVS" | grep '^POSTGRES_USER=' | cut -d'=' -f2-)
        PG_PASS=$(echo "$ENVS" | grep '^POSTGRES_PASSWORD=' | cut -d'=' -f2-)
        [ -n "$PG_PASS" ] && CRED_USER="${PG_USER:-postgres}" && CRED_PASS="$PG_PASS"

        # Redis
        if echo "$cimage" | grep -qi "redis"; then
            REDIS_PASS=$(docker inspect --format '{{join .Args " "}}' "$cname" 2>/dev/null | sed -n 's/.*--requirepass \([^ ]*\).*/\1/p')
            [ -n "$REDIS_PASS" ] && CRED_USER="(no user)" && CRED_PASS="$REDIS_PASS"
        fi

        # Hiển thị credentials nếu có
        if [ -n "$CRED_PASS" ]; then
            echo -e "${CYAN}│${NC}  Auth:  User=${GREEN}$CRED_USER${NC}  Pass=${GREEN}$CRED_PASS${NC}"
        fi
    done
    echo -e "${CYAN}└─────────────────────────────────────────${NC}"
}

# ===== DOCKER: TẠO CONTAINER =====
docker_create_container() {
    if ! command -v docker >/dev/null 2>&1; then
        echo -e "${RED}❌ Docker chưa được cài đặt. Chọn mục 1 để cài.${NC}"
        return
    fi

    while true; do
        clear
        echo -e "${CYAN}====================================================${NC}"
        echo -e "${YELLOW}          🐳 TẠO CONTAINER MỚI${NC}"
        echo -e "${CYAN}====================================================${NC}"
        echo -e "   ${YELLOW}1.${NC} MySQL       (Database quan hệ)"
        echo -e "   ${YELLOW}2.${NC} MongoDB     (Database NoSQL)"
        echo -e "   ${YELLOW}3.${NC} Redis       (Cache / Message Broker)"
        echo -e "   ${YELLOW}4.${NC} Qdrant      (Vector Database cho AI)"
        echo -e "   ${YELLOW}5.${NC} n8n         (Workflow Automation)"
        echo -e "   ${YELLOW}6.${NC} Custom      (Tự nhập image Docker)"
        echo -e "${CYAN}----------------------------------------------------${NC}"
        echo -e "   ${YELLOW}0.${NC} Quay lại"
        echo -e "${CYAN}====================================================${NC}"

        read -p " ➔ Chọn dịch vụ: " svc
        [[ "$svc" == "0" || -z "$svc" ]] && break

        case $svc in
            1) _docker_create_mysql ;;
            2) _docker_create_mongodb ;;
            3) _docker_create_redis ;;
            4) _docker_create_qdrant ;;
            5) _docker_create_n8n ;;
            6) _docker_create_custom ;;
            *) echo -e "${RED}❌ Lựa chọn không hợp lệ!${NC}"; sleep 1 ;;
        esac
    done
}

# --- Helper: Tạo giá trị ngẫu nhiên ---
_rand_port() {
    # Tạo port ngẫu nhiên trong khoảng 10000-49999, tránh trùng port đang dùng
    while true; do
        p=$((RANDOM % 40000 + 10000))
        if ! docker ps -a --format '{{.Ports}}' 2>/dev/null | grep -q ":${p}->"; then
            echo "$p"; return
        fi
    done
}
_rand_pass() {
    # Tạo mật khẩu ngẫu nhiên 16 ký tự (chữ + số)
    cat /dev/urandom | tr -dc 'a-zA-Z0-9' | head -c 16
}
_rand_user() {
    # Tạo username ngẫu nhiên: admin_ + 5 ký tự
    echo "admin_$(cat /dev/urandom | tr -dc 'a-z0-9' | head -c 5)"
}

# Kiểm tra volume đã tồn tại (quan trọng: DB chỉ tạo user lần đầu khi volume trống)
# Trả về 0 = OK tiếp tục, 1 = user hủy
_check_volume() {
    local vol_name="$1"
    if docker volume inspect "$vol_name" >/dev/null 2>&1; then
        echo -e "\n${YELLOW}⚠ Volume [$vol_name] đã tồn tại từ lần tạo trước!${NC}"
        echo -e "${YELLOW}  Với MySQL/MongoDB: mật khẩu mới sẽ BỊ BỎ QUA vì data cũ đã có user.${NC}"
        echo -e "  1. ${GREEN}Giữ nguyên${NC} volume cũ (dùng credentials cũ)"
        echo -e "  2. ${RED}Xóa volume${NC} cũ → tạo mới hoàn toàn (MẤT DỮ LIỆU)"
        echo -e "  3. Hủy, không tạo container"
        read -p " ➔ Chọn (1/2/3): " vol_choice
        case $vol_choice in
            2)
                docker volume rm "$vol_name" 2>/dev/null
                echo -e "${GREEN}✔ Đã xóa volume cũ [$vol_name]${NC}"
                return 0
                ;;
            3) return 1 ;;
            *) 
                echo -e "${CYAN}ℹ Giữ nguyên volume cũ. Credentials mới sẽ không có hiệu lực.${NC}"
                return 0
                ;;
        esac
    fi
    return 0
}

# Tự động thêm TCP proxy vào FRP config cho port container
# Dùng: _auto_add_proxy <local_port> [<tên gợi nhớ>]
_auto_add_proxy() {
    local lport="$1"
    local label="${2:-container}"
    
    echo ""
    read -p "Bạn có muốn tạo FRP Proxy TCP để truy cập từ bên ngoài? (Y/n): " add_proxy
    if [[ "$add_proxy" =~ ^[Nn]$ ]]; then
        return
    fi

    # Tạo remote port ngẫu nhiên
    local remote_port
    while true; do
        remote_port=$((RANDOM % 40000 + 10000))
        if ! grep -q "remotePort = $remote_port" "$FRPC_CONF"; then
            break
        fi
    done

    cat >> "$FRPC_CONF" <<EOL

[[proxies]]
name = "tcp_${label}_${lport}"
type = "tcp"
localIP = "127.0.0.1"
localPort = $lport
remotePort = $remote_port
EOL

    echo -e "${GREEN}✔ Đã thêm FRP Proxy TCP: Local $lport → FRP $remote_port${NC}"

    # Gọi API mở port trên server
    SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
    API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
    API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
    if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
        curl -s -X POST -d "token=$API_TOKEN&port=$remote_port&action=add_port" "http://$SERVER_IP:$API_PORT" >/dev/null 2>&1
    fi

    reload_frpc

    echo -e "${CYAN}   Truy cập từ bên ngoài: ${YELLOW}$SERVER_IP:$remote_port${NC}"
}

# --- MySQL ---
_docker_create_mysql() {
    echo -e "\n${CYAN}--- TẠO CONTAINER MYSQL ---${NC}"
    echo -e "${YELLOW}(Nhấn Enter để tạo giá trị ngẫu nhiên)${NC}\n"
    read -p "Tên container (mặc định: mysql): " name
    name=${name:-mysql}
    _check_volume "${name}_data" || return
    
    DEF_PORT=$(_rand_port)
    read -p "Port ánh xạ (Enter = random $DEF_PORT): " port
    port=${port:-$DEF_PORT}
    
    DEF_PASS=$(_rand_pass)
    read -s -p "Mật khẩu root (Enter = random): " password
    echo ""
    password=${password:-$DEF_PASS}
    
    read -p "Phiên bản MySQL (mặc định: latest): " version
    version=${version:-latest}

    echo -e "\n${YELLOW}Đang tạo container $name...${NC}"
    docker run -d \
        --name "$name" \
        --restart unless-stopped \
        -p "$port":3306 \
        -v "${name}_data":/var/lib/mysql \
        -e MYSQL_ROOT_PASSWORD="$password" \
        mysql:"$version"

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container MySQL [$name] đã tạo thành công!${NC}"
        echo -e "   Port:     ${CYAN}$port${NC}"
        echo -e "   User:     ${CYAN}root${NC}"
        echo -e "   Password: ${CYAN}$password${NC}"
        echo -e "   Volume:   ${CYAN}${name}_data${NC}"
        _auto_add_proxy "$port" "mysql"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# --- MongoDB ---
_docker_create_mongodb() {
    echo -e "\n${CYAN}--- TẠO CONTAINER MONGODB ---${NC}"
    echo -e "${YELLOW}(Nhấn Enter để tạo giá trị ngẫu nhiên)${NC}\n"
    read -p "Tên container (mặc định: mongodb): " name
    name=${name:-mongodb}
    _check_volume "${name}_data" || return
    
    DEF_PORT=$(_rand_port)
    read -p "Port ánh xạ (Enter = random $DEF_PORT): " port
    port=${port:-$DEF_PORT}
    
    DEF_USER=$(_rand_user)
    read -p "Username admin (Enter = random $DEF_USER): " username
    username=${username:-$DEF_USER}
    
    DEF_PASS=$(_rand_pass)
    read -s -p "Mật khẩu admin (Enter = random): " password
    echo ""
    password=${password:-$DEF_PASS}

    echo -e "\n${YELLOW}Đang tạo container $name...${NC}"
    docker run -d \
        --name "$name" \
        --restart unless-stopped \
        -p "$port":27017 \
        -v "${name}_data":/data/db \
        -e MONGO_INITDB_ROOT_USERNAME="$username" \
        -e MONGO_INITDB_ROOT_PASSWORD="$password" \
        mongo:latest

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container MongoDB [$name] đã tạo thành công!${NC}"
        echo -e "   Port:     ${CYAN}$port${NC}"
        echo -e "   User:     ${CYAN}$username${NC}"
        echo -e "   Password: ${CYAN}$password${NC}"
        echo -e "   Volume:   ${CYAN}${name}_data${NC}"
        _auto_add_proxy "$port" "mongo"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# --- Redis ---
_docker_create_redis() {
    echo -e "\n${CYAN}--- TẠO CONTAINER REDIS ---${NC}"
    echo -e "${YELLOW}(Nhấn Enter để tạo giá trị ngẫu nhiên)${NC}\n"
    read -p "Tên container (mặc định: redis): " name
    name=${name:-redis}
    _check_volume "${name}_data" || return
    
    DEF_PORT=$(_rand_port)
    read -p "Port ánh xạ (Enter = random $DEF_PORT): " port
    port=${port:-$DEF_PORT}
    
    DEF_PASS=$(_rand_pass)
    read -s -p "Mật khẩu (Enter = random, gõ 'none' = không mật khẩu): " password
    echo ""
    
    REDIS_CMD=""
    if [ "$password" = "none" ]; then
        password=""
    else
        password=${password:-$DEF_PASS}
        REDIS_CMD="--requirepass $password"
    fi

    echo -e "\n${YELLOW}Đang tạo container $name...${NC}"
    docker run -d \
        --name "$name" \
        --restart unless-stopped \
        -p "$port":6379 \
        -v "${name}_data":/data \
        redis:latest $REDIS_CMD

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container Redis [$name] đã tạo thành công!${NC}"
        echo -e "   Port:     ${CYAN}$port${NC}"
        if [ -n "$password" ]; then
            echo -e "   Password: ${CYAN}$password${NC}"
        else
            echo -e "   Password: ${YELLOW}(không đặt)${NC}"
        fi
        echo -e "   Volume:   ${CYAN}${name}_data${NC}"
        _auto_add_proxy "$port" "redis"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# --- Qdrant ---
_docker_create_qdrant() {
    echo -e "\n${CYAN}--- TẠO CONTAINER QDRANT (Vector DB) ---${NC}"
    echo -e "${YELLOW}(Nhấn Enter để tạo giá trị ngẫu nhiên)${NC}\n"
    read -p "Tên container (mặc định: qdrant): " name
    name=${name:-qdrant}
    
    DEF_HTTP=$(_rand_port)
    read -p "Port HTTP API (Enter = random $DEF_HTTP): " port_http
    port_http=${port_http:-$DEF_HTTP}
    
    DEF_GRPC=$(_rand_port)
    read -p "Port gRPC (Enter = random $DEF_GRPC): " port_grpc
    port_grpc=${port_grpc:-$DEF_GRPC}

    echo -e "\n${YELLOW}Đang tạo container $name...${NC}"
    docker run -d \
        --name "$name" \
        --restart unless-stopped \
        -p "$port_http":6333 \
        -p "$port_grpc":6334 \
        -v "${name}_data":/qdrant/storage \
        qdrant/qdrant:latest

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container Qdrant [$name] đã tạo thành công!${NC}"
        echo -e "   HTTP API:  ${CYAN}http://localhost:$port_http${NC}"
        echo -e "   gRPC:      ${CYAN}localhost:$port_grpc${NC}"
        echo -e "   Dashboard: ${CYAN}http://localhost:$port_http/dashboard${NC}"
        echo -e "   Volume:    ${CYAN}${name}_data${NC}"
        _auto_add_proxy "$port_http" "qdrant_http"
        _auto_add_proxy "$port_grpc" "qdrant_grpc"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# --- n8n ---
_docker_create_n8n() {
    echo -e "\n${CYAN}--- TẠO CONTAINER n8n (Workflow Automation) ---${NC}"
    echo -e "${YELLOW}(Nhấn Enter để tạo giá trị ngẫu nhiên)${NC}\n"
    read -p "Tên container (mặc định: n8n): " name
    name=${name:-n8n}
    
    DEF_PORT=$(_rand_port)
    read -p "Port ánh xạ (Enter = random $DEF_PORT): " port
    port=${port:-$DEF_PORT}

    echo -e "\n${YELLOW}Đang tạo container $name...${NC}"
    docker run -d \
        --name "$name" \
        --restart unless-stopped \
        -p "$port":5678 \
        -v "${name}_data":/home/node/.n8n \
        -e GENERIC_TIMEZONE="Asia/Ho_Chi_Minh" \
        -e TZ="Asia/Ho_Chi_Minh" \
        n8nio/n8n:latest

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container n8n [$name] đã tạo thành công!${NC}"
        echo -e "   Web UI: ${CYAN}http://localhost:$port${NC}"
        echo -e "   Volume: ${CYAN}${name}_data${NC}"
        _auto_add_proxy "$port" "n8n"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# --- Custom Image ---
_docker_create_custom() {
    echo -e "\n${CYAN}--- TẠO CONTAINER TỪ IMAGE TÙY CHỌN ---${NC}"
    read -p "Tên image (VD: nginx:latest, postgres:16): " image
    [[ -z "$image" ]] && return
    read -p "Tên container: " name
    [[ -z "$name" ]] && return
    read -p "Port ánh xạ (VD: 8080:80, để trống nếu không cần): " port_map
    read -p "Biến môi trường (VD: KEY=val,KEY2=val2, để trống nếu không cần): " env_vars

    CMD="docker run -d --name \"$name\" --restart unless-stopped"
    [ -n "$port_map" ] && CMD="$CMD -p $port_map"
    CMD="$CMD -v ${name}_data:/data"

    # Thêm biến môi trường
    if [ -n "$env_vars" ]; then
        IFS=',' read -ra ENVS <<< "$env_vars"
        for e in "${ENVS[@]}"; do
            CMD="$CMD -e $e"
        done
    fi

    CMD="$CMD $image"

    echo -e "${YELLOW}Đang chạy: $CMD${NC}"
    eval $CMD

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✔ Container [$name] đã tạo thành công!${NC}"
    else
        echo -e "${RED}❌ Tạo container thất bại!${NC}"
    fi
    echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
}

# ===== DOCKER: QUẢN LÝ CONTAINER =====
docker_manage_container() {
    if ! command -v docker >/dev/null 2>&1; then
        echo -e "${RED}❌ Docker chưa được cài đặt.${NC}"
        return
    fi

    while true; do
        clear
        docker_list_containers
        echo -e "\n${CYAN}--- QUẢN LÝ CONTAINER ---${NC}"
        echo -e "   ${YELLOW}1.${NC} Khởi động container"
        echo -e "   ${YELLOW}2.${NC} Dừng container"
        echo -e "   ${YELLOW}3.${NC} Khởi động lại container"
        echo -e "   ${YELLOW}4.${NC} Xem logs container"
        echo -e "   ${YELLOW}5.${NC} Xóa container"
        echo -e "   ${YELLOW}6.${NC} Vào shell container (exec bash)"
        echo -e "   ${YELLOW}0.${NC} Quay lại"

        read -p " ➔ Chọn: " mc
        [[ "$mc" == "0" || -z "$mc" ]] && break

        case $mc in
            1)
                read -p "Tên container cần khởi động: " cn
                [ -n "$cn" ] && docker start "$cn" && echo -e "${GREEN}✔ Đã khởi động $cn${NC}"
                ;;
            2)
                read -p "Tên container cần dừng: " cn
                [ -n "$cn" ] && docker stop "$cn" && echo -e "${GREEN}✔ Đã dừng $cn${NC}"
                ;;
            3)
                read -p "Tên container cần restart: " cn
                [ -n "$cn" ] && docker restart "$cn" && echo -e "${GREEN}✔ Đã restart $cn${NC}"
                ;;
            4)
                read -p "Tên container cần xem logs: " cn
                [ -n "$cn" ] && docker logs --tail 50 "$cn" 2>&1
                ;;
            5)
                read -p "Tên container cần xóa: " cn
                if [ -n "$cn" ]; then
                    # Lấy local ports của container để xóa FRP proxy liên quan
                    C_PORTS=$(docker inspect --format '{{range $p, $conf := .HostConfig.PortBindings}}{{range $conf}}{{.HostPort}} {{end}}{{end}}' "$cn" 2>/dev/null)

                    read -p "Xóa luôn volume dữ liệu? (y/N): " rm_vol
                    docker stop "$cn" >/dev/null 2>&1
                    if [[ "$rm_vol" =~ ^[Yy]$ ]]; then
                        docker rm -v "$cn" >/dev/null 2>&1
                        echo -e "${GREEN}✔ Đã xóa container + volume $cn${NC}"
                    else
                        docker rm "$cn" >/dev/null 2>&1
                        echo -e "${GREEN}✔ Đã xóa container $cn (giữ lại volume)${NC}"
                    fi

                    # Xóa FRP proxy liên quan đến các port của container
                    if [ -n "$C_PORTS" ]; then
                        for cp in $C_PORTS; do
                            # Tìm remotePort trước khi xóa (để gọi API đóng port)
                            REMOTE_P=$(awk -v p="$cp" '
                                /^\[\[proxies\]\]/ { rp=""; found=0 }
                                $1=="localPort" && $3==p { found=1 }
                                $1=="remotePort" { rp=$3 }
                                END { if (found && rp!="") print rp }
                            ' "$FRPC_CONF" 2>/dev/null)

                            # Xóa proxy block khỏi frpc.toml
                            awk -v p="$cp" '
                                /^\[\[proxies\]\]/ { block=""; in_block=1; match_port=0 }
                                in_block { block = block $0 "\n" }
                                in_block && $1=="localPort" && $3==p { match_port=1 }
                                /^$/ || /^\[/ && !/^\[\[proxies\]\]/ {
                                    if (in_block && !match_port) printf "%s", block
                                    if (!in_block) print $0
                                    in_block=0; block=""
                                    next
                                }
                                !in_block { print $0 }
                                END { if (in_block && !match_port) printf "%s", block }
                            ' "$FRPC_CONF" > "${FRPC_CONF}.tmp" && mv "${FRPC_CONF}.tmp" "$FRPC_CONF"

                            if [ -n "$REMOTE_P" ]; then
                                echo -e "${GREEN}✔ Đã xóa FRP Proxy (Local $cp → FRP $REMOTE_P)${NC}"
                                # Gọi API đóng port trên server
                                SERVER_IP=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
                                API_PORT=$(grep -E "^# API_PORT=" "$FRPC_CONF" | cut -d'=' -f2)
                                API_TOKEN=$(grep -E "^# API_TOKEN=" "$FRPC_CONF" | cut -d'=' -f2)
                                if [[ -n "$SERVER_IP" && -n "$API_PORT" && -n "$API_TOKEN" ]]; then
                                    curl -s -X POST -d "token=$API_TOKEN&port=$REMOTE_P&action=remove_port" "http://$SERVER_IP:$API_PORT" >/dev/null 2>&1
                                fi
                            fi
                        done
                        reload_frpc
                    fi
                fi
                ;;
            6)
                read -p "Tên container: " cn
                if [ -n "$cn" ]; then
                    echo -e "${YELLOW}Đang kết nối vào $cn (gõ 'exit' để thoát)...${NC}"
                    docker exec -it "$cn" bash 2>/dev/null || docker exec -it "$cn" sh
                fi
                ;;
            *) echo -e "${RED}❌ Không hợp lệ!${NC}"; sleep 1 ;;
        esac
        echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..."
    done
}

# ===== DOCKER: MENU CHÍNH =====
docker_menu() {
    while true; do
        clear
        echo -e "${CYAN}====================================================${NC}"
        echo -e "${YELLOW}              🐳 QUẢN LÝ DOCKER${NC}"
        echo -e "${CYAN}====================================================${NC}"

        # Hiển thị trạng thái Docker
        if command -v docker >/dev/null 2>&1; then
            DOCKER_VER=$(docker --version 2>/dev/null | sed 's/Docker version //' | cut -d',' -f1)
            RUNNING=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
            echo -e "   Docker: ${GREEN}v$DOCKER_VER${NC} | Container đang chạy: ${GREEN}$RUNNING${NC}"
        else
            echo -e "   Docker: ${RED}[ CHƯA CÀI ĐẶT ]${NC}"
        fi

        echo -e "${CYAN}----------------------------------------------------${NC}"
        echo -e "   ${YELLOW}1.${NC} Cài đặt / Cập nhật Docker"
        echo -e "   ${YELLOW}2.${NC} Danh sách Container"
        echo -e "   ${YELLOW}3.${NC} Tạo Container mới (MySQL, Mongo, Redis...)"
        echo -e "   ${YELLOW}4.${NC} Quản lý Container (Start/Stop/Xóa/Logs)"
        echo -e "${CYAN}----------------------------------------------------${NC}"
        echo -e "   ${YELLOW}0.${NC} Quay lại Menu chính"
        echo -e "${CYAN}====================================================${NC}"

        read -p " ➔ Chọn: " dc
        case $dc in
            1) install_docker; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..." ;;
            2) docker_list_containers; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ..." ;;
            3) docker_create_container ;;
            4) docker_manage_container ;;
            0) break ;;
            *) echo -e "${RED}❌ Không hợp lệ!${NC}"; sleep 1 ;;
        esac
    done
}

# ===== SHOW MENU =====
show_menu() {
    clear
    echo -e "${CYAN}====================================================${NC}"
    echo -e "${YELLOW}      CÔNG CỤ QUẢN LÝ FRPC CLIENT - PHIÊN BẢN 0.2.6${NC}"
    echo -e "${CYAN}====================================================${NC}"
    
    HAS_SERVER=$(grep -E "^serverAddr" "$FRPC_CONF" | cut -d'"' -f2)
    
    if [[ -z "$HAS_SERVER" ]]; then
        echo -e "   FRPC: ${RED}[ CHƯA CẤU HÌNH ]${NC} 🔴"
    else
        if pm2 list --no-color 2>/dev/null | grep -w "$PM2_NAME" | grep -qw "online"; then
            echo -e "   FRPC: ${GREEN}[ ONLINE ]${NC} 🟢"
        else
            echo -e "   FRPC: ${RED}[ OFFLINE ]${NC} 🔴"
        fi
    fi

    # Docker status
    if command -v docker >/dev/null 2>&1; then
        DOCK_COUNT=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
        echo -e "   Docker: ${GREEN}[ OK ]${NC} 🐳  Container: ${GREEN}$DOCK_COUNT${NC} đang chạy"
    else
        echo -e "   Docker: ${RED}[ CHƯA CÀI ]${NC}"
    fi

    echo -e "${CYAN}----------------------------------------------------${NC}"
    
    echo -e "   ${YELLOW}1.${NC} Cài đặt FRPC"
    echo -e "   ${YELLOW}2.${NC} Cấu hình FRPC"
    echo -e "   ${YELLOW}3.${NC} Khởi động hoặc Restart FRPC (Có check kết nối)"
    echo -e "   ${YELLOW}4.${NC} Thêm Domain / Mở Port"
    echo -e "   ${YELLOW}5.${NC} Xem danh sách Domain / Port"
    echo -e "   ${YELLOW}6.${NC} Quản lý Docker & Container"
    echo -e "${CYAN}----------------------------------------------------${NC}"
    echo -e "   ${YELLOW}0.${NC} Thoát"
    echo -e "${CYAN}====================================================${NC}"
}

# ===== MAIN LOOP =====
while true; do
    show_menu
    read -p " ➔ Nhập lựa chọn của bạn: " c

    case $c in
        1) install_frp; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ...";;
        2) config_server; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ...";;
        3) run_frpc; test_connection; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ...";;
        4) add_domain ;;
        5) list_domains; echo ""; read -n 1 -s -r -p "Nhấn phím bất kỳ để tiếp tục...";;
        6) docker_menu ;;
        0) 
            echo -e "\n${GREEN}Tạm biệt! Chúc bạn dùng FRP vui vẻ.${NC}\n"
            exit 0 
            ;;
        *) 
            echo -e "\n${RED}❌ Lựa chọn không hợp lệ!${NC}"
            sleep 1.5
            ;;
    esac
done
