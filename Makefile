BINARY := elk-diagnostics
DIST_DIR := dist
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# 版本 pin 死，不用 @latest：SBOM 產出必須可重現，工具本身升版也不該在
# 客戶不知情的情況下悄悄改變交付內容。
CYCLONEDX_GOMOD := github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0

# 政府/金融客戶常見的 Linux 機型：x86_64 與 ARM（如 AWS Graviton）。
TARGETS := linux/amd64 linux/arm64

.PHONY: build generate dist clean

# 本機開發用：跑在你目前這台機器上
build:
	go build -trimpath -o $(BINARY) ./cmd/elk-diagnostics

# 重新產生 checked in 的交付物。端點表（internal/collector/endpoints.go）或
# collect.sh.tmpl 有變動時必跑，否則過期檢查測試會擋下。
# API清單.md 一併 checked in，好處是新增端點時 diff 會直接顯示
# 「對客戶叢集的 API 呼叫面變了」——那是該被看見的審查訊號。
generate:
	go run ./cmd/elk-diagnostics collect-script > collect.sh
	chmod +x collect.sh
	go run ./cmd/elk-diagnostics apis --output markdown > docs/交付/API清單.md
	@echo "已更新 collect.sh、docs/交付/API清單.md"

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
	@# 交付物不只有二進位檔：客戶不允許執行未知執行檔時，走的是採集腳本這條路，
	@# 而 API 清單是導入審查會要的文件（見 docs/內部/規格/採集包規格.md §2）。
	cp collect.sh $(DIST_DIR)/collect.sh
	cp docs/交付/API清單.md $(DIST_DIR)/API清單.md
	cp docs/交付/使用手冊.md $(DIST_DIR)/使用手冊.md
	(cd $(DIST_DIR) && shasum -a 256 collect.sh > collect.sh.sha256)
	(cd $(DIST_DIR) && shasum -a 256 API清單.md > API清單.md.sha256)
	(cd $(DIST_DIR) && shasum -a 256 使用手冊.md > 使用手冊.md.sha256)
	@# SBOM（CycloneDX）：導入審查清單最後一個缺口。module 層級即可，記錄本工具
	@# 與全部相依套件的版本，供客戶資安做已知漏洞（CVE）比對。
	go run $(CYCLONEDX_GOMOD) mod -json -output $(DIST_DIR)/sbom.cdx.json .
	(cd $(DIST_DIR) && shasum -a 256 sbom.cdx.json > sbom.cdx.json.sha256)
	@echo
	@echo "產出（commit $(VERSION)）："
	@for f in $(DIST_DIR)/*.sha256; do cat $$f; done
	@echo "  $(DIST_DIR)/API清單.md（供客戶資安/導入審查）"
	@echo "  $(DIST_DIR)/使用手冊.md（客戶操作手冊）"
	@echo "  $(DIST_DIR)/sbom.cdx.json（CycloneDX SBOM，供客戶資安/導入審查）"

clean:
	rm -rf $(DIST_DIR) $(BINARY)
