.PHONY: all clean build-linux-amd64 build-linux-arm64 bump-version

BINARY_NAME=nalink
BUILD_DATE=$(shell date +%Y-%m-%d)

# Đọc version hiện tại và tự tăng patch
CURRENT_VERSION := $(shell cat VERSION 2>/dev/null || echo "0.0.0")
MAJOR := $(shell echo $(CURRENT_VERSION) | cut -d. -f1)
MINOR := $(shell echo $(CURRENT_VERSION) | cut -d. -f2)
PATCH := $(shell echo $(CURRENT_VERSION) | cut -d. -f3)
VERSION := $(shell \
	M=$(MAJOR); m=$(MINOR); p=$(PATCH); \
	p=$$((p+1)); \
	if [ $$p -gt 99 ]; then p=0; m=$$((m+1)); fi; \
	if [ $$m -gt 99 ]; then m=0; M=$$((M+1)); fi; \
	echo "$$M.$$m.$$p")

LDFLAGS := -ldflags="-w -s -X github.com/DangLong/na-server-go/pkg/cli.AppVersion=v$(VERSION) -X github.com/DangLong/na-server-go/pkg/cli.BuildDate=$(BUILD_DATE)"

all: clean bump-version build-linux-amd64 build-linux-arm64
	@echo "✅ Build xong tất cả platforms - v$(VERSION) - $(BUILD_DATE)"

bump-version:
	@echo "$(VERSION)" > VERSION
	@echo "📦 Version: v$(VERSION) - $(BUILD_DATE)"
	@mkdir -p build/
	@echo '{"version": "v$(VERSION)", "date": "$(BUILD_DATE)"}' > build/version.json

build-linux-amd64:
	@echo "Building for Linux amd64 (x86_64)..."
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-Linux-x86_64 main.go

build-linux-arm64:
	@echo "Building for Linux arm64 (aarch64 / arm64)..."
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o build/$(BINARY_NAME)-Linux-aarch64 main.go

clean:
	@echo "Cleaning output directory..."
	@rm -rf build/
	@mkdir -p build/

