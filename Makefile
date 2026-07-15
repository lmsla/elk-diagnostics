BINARY := elk-diagnostics
DIST_DIR := dist
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# 政府/金融客戶常見的 Linux 機型：x86_64 與 ARM（如 AWS Graviton）。
TARGETS := linux/amd64 linux/arm64

.PHONY: build dist clean

# 本機開發用：跑在你目前這台機器上
build:
	go build -trimpath -o $(BINARY) ./cmd/elk-diagnostics

# 交付用：靜態連結、無 libc 動態相依（適合客戶 Linux VM，含未知 glibc 版本），
# 逐 GOOS/GOARCH 產出二進位 + 各自的 SHA256 checksum。
# -trimpath 讓建置可重現、去除本機路徑差異；連 git commit 一併嵌入二進位（go version -m 可查）
dist:
	mkdir -p $(DIST_DIR)
	for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		out=$(DIST_DIR)/$(BINARY)-$$os-$$arch; \
		echo "建置 $$os/$$arch..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -o $$out ./cmd/elk-diagnostics; \
		(cd $(DIST_DIR) && shasum -a 256 $(BINARY)-$$os-$$arch > $(BINARY)-$$os-$$arch.sha256); \
	done
	@echo "產出（commit $(VERSION)）："
	@for f in $(DIST_DIR)/*.sha256; do cat $$f; done

clean:
	rm -rf $(DIST_DIR) $(BINARY)
