#!/bin/bash

# Màu sắc hiển thị
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

clear
echo -e "${CYAN}====================================================${NC}"
echo -e "${YELLOW}🚀 CÔNG CỤ DEPLOY SERVER LÊN VPS (SSH AUTO)${NC}"
echo -e "${CYAN}====================================================${NC}"

# 1. Nhập thông tin VPS
read -p "Nhập địa chỉ IP của VPS [180.93.35.170]: " VPS_IP
VPS_IP=${VPS_IP:-180.93.35.170}

read -p "Nhập Username [root]: " VPS_USER
VPS_USER=${VPS_USER:-root}

read -p "Nhập Password (ẩn) [Mặc định]: " -s VPS_PASS
echo ""
VPS_PASS=${VPS_PASS:-AZvpsB!8G855gL!}

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/server"

echo -e "${YELLOW}>>> Đang Build lại mã nguồn cho Linux...${NC}"
cd "$SOURCE_DIR"
GOOS=linux GOARCH=amd64 go build -o nalink-server main.go
cd - >/dev/null
DEST_DIR="/root/nalink-server-ha"

if [ ! -d "$SOURCE_DIR" ]; then
    echo -e "${RED}❌ Không tìm thấy thư mục $SOURCE_DIR${NC}"
    exit 1
fi

echo -e "\n${CYAN}>>> Bắt đầu đẩy mã nguồn lên VPS ($VPS_IP)...${NC}"

# 2. Tạo file Expect tạm thời để tự động hóa SCP
EXPECT_SCRIPT="/tmp/deploy_vps_$$.exp"

cat << 'EOF' > "$EXPECT_SCRIPT"
#!/usr/bin/expect -f
set timeout -1
set VPS_IP [lindex $argv 0]
set VPS_USER [lindex $argv 1]
set VPS_PASS [lindex $argv 2]
set SOURCE_FILE [lindex $argv 3]
set DEST_DIR [lindex $argv 4]

# Create directory on remote server first and remove old binary
spawn ssh -o StrictHostKeyChecking=no $VPS_USER@$VPS_IP "mkdir -p $DEST_DIR && rm -f $DEST_DIR/nalink-server"
expect {
    "password:" {
        send "$VPS_PASS\r"
    }
    "yes/no" {
        send "yes\r"
        expect "password:"
        send "$VPS_PASS\r"
    }
}
expect eof

# Now copy only the binary file
spawn scp -o StrictHostKeyChecking=no $SOURCE_FILE $VPS_USER@$VPS_IP:$DEST_DIR/nalink-server
expect {
    "password:" {
        send "$VPS_PASS\r"
    }
    "yes/no" {
        send "yes\r"
        expect "password:"
        send "$VPS_PASS\r"
    }
}
expect eof
EOF

chmod +x "$EXPECT_SCRIPT"

# 3. Chạy lệnh SCP tự động
"$EXPECT_SCRIPT" "$VPS_IP" "$VPS_USER" "$VPS_PASS" "$SOURCE_DIR/nalink-server" "$DEST_DIR"

rm -f "$EXPECT_SCRIPT"

echo -e "\n${GREEN}✔ Đã tải mã nguồn Server lên $VPS_IP thành công!${NC}"
echo -e "${YELLOW}Thư mục lưu trữ: ${CYAN}$DEST_DIR${NC}"

# 4. Hỏi xem có muốn SSH tự động vào luôn không
echo ""
read -p "Bạn có muốn tự động SSH vào VPS để chạy Server ngay bây giờ? (y/N): " choice
if [[ "$choice" =~ ^[Yy]$ ]]; then
    echo -e "${CYAN}>>> Đang kết nối SSH...${NC}"
    
    # Tạo Expect để SSH vào và chuyển hướng tới thư mục code
    SSH_EXPECT="/tmp/ssh_auto_$$.exp"
    cat << 'EOF' > "$SSH_EXPECT"
#!/usr/bin/expect -f
set timeout -1
set VPS_IP [lindex $argv 0]
set VPS_USER [lindex $argv 1]
set VPS_PASS [lindex $argv 2]
set DEST_DIR [lindex $argv 3]

spawn ssh -o StrictHostKeyChecking=no $VPS_USER@$VPS_IP
expect {
    "password:" {
        send "$VPS_PASS\r"
    }
    "yes/no" {
        send "yes\r"
        expect "password:"
        send "$VPS_PASS\r"
    }
}
expect -re {[\$#] }
send "cd $DEST_DIR && chmod +x nalink-server\r"
expect -re {[\$#] }
send "clear && echo '=== ĐÃ SẴN SÀNG, BẠN CÓ THỂ CHẠY LỆNH: ./nalink-server ==='\r"
interact
EOF
    chmod +x "$SSH_EXPECT"
    "$SSH_EXPECT" "$VPS_IP" "$VPS_USER" "$VPS_PASS" "$DEST_DIR"
    rm -f "$SSH_EXPECT"
else
    echo -e "${GREEN}Hoàn tất! Cảm ơn bạn.${NC}"
fi
