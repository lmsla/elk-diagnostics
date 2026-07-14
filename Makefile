BINARY := elk-doctor
DIST_DIR := dist
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: build dist clean

# 本機開發用：跑在你目前這台機器上
build:
	go build -trimpath -o $(BINARY) ./cmd/elk-doctor

# 交付用：靜態連結、無 libc 動態相依，適合客戶 Linux VM（含未知 glibc 版本）
# -trimpath 讓建置可重現、去除本機路徑差異；連 git commit 一併嵌入二進位（go version -m 可查）
dist:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
		-o $(DIST_DIR)/$(BINARY)-linux-amd64 ./cmd/elk-doctor
	cd $(DIST_DIR) && shasum -a 256 $(BINARY)-linux-amd64 > $(BINARY)-linux-amd64.sha256
	@echo "產出：$(DIST_DIR)/$(BINARY)-linux-amd64（commit $(VERSION)）"
	@cat $(DIST_DIR)/$(BINARY)-linux-amd64.sha256

clean:
	rm -rf $(DIST_DIR) $(BINARY)
