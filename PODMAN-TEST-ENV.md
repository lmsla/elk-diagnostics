# Podman 測試環境指引（給 agent）

**目的**：在 macOS ＋ Podman 的機器上（無 Docker），啟動本專案的標準測試環境——
ES 8.14.3、ES 9.0.0 各一個單節點，以及對應版本的 Kibana——然後走完整健檢測試流程。

**讀者是 agent**：每一步都有「預期結果」，結果不符就停下來按〈疑難排解〉處理，不要跳過往下走。
版本、埠號、容器名刻意與 `dev/phase0/docker-compose.yml` 一致，讓 `docs/VERIFICATION.md` 的
造壓配方與既有文件可以原樣沿用。

| 服務 | 容器名 | 對外埠 | 版本 |
|---|---|---|---|
| ES 8 | `elkdoctor-es8` | 9208 | 8.14.3 |
| Kibana 8 | `elkdoctor-kibana8` | 5601 | 8.14.3 |
| ES 9 | `elkdoctor-es9` | 9209 | 9.0.0 |
| Kibana 9 | `elkdoctor-kibana9` | 5602 | 9.0.0 |

僅供本機開發測試：security 關閉、資料不落地（容器刪掉即消失）。**請勿用於正式環境。**

---

## 1. Podman machine（macOS 特有）

macOS 上 Podman 的容器跑在一個 Linux VM 裡，兩件事必須先確認：

```bash
# 1a. VM 存在且在跑（記憶體給 6GB 以上：2 個 ES 各 512MB heap + 2 個 Kibana 約 1GB + 餘裕）
podman machine ls
# 若不存在：
podman machine init --memory 6144 --cpus 4
podman machine start

# 1b. ES 硬性要求 vm.max_map_count ≥ 262144，Podman VM 預設值不夠，必須進 VM 設定：
podman machine ssh "sudo sysctl -w vm.max_map_count=262144"
```

**預期**：`podman machine ls` 顯示 `Currently running`；sysctl 回 `vm.max_map_count = 262144`。

> 注意：`podman machine ssh` 設定的 sysctl 在 VM 重開後會消失。若 ES 起不來且 log 出現
> `max virtual memory areas vm.max_map_count [65530] is too low`，回到這一步重設。

## 2. 啟動 ES（先 ES，後 Kibana）

```bash
podman run -d --name elkdoctor-es8 -p 9208:9200 \
  -e discovery.type=single-node \
  -e xpack.security.enabled=false \
  -e xpack.security.http.ssl.enabled=false \
  -e "ES_JAVA_OPTS=-Xms512m -Xmx512m" \
  docker.elastic.co/elasticsearch/elasticsearch:8.14.3

podman run -d --name elkdoctor-es9 -p 9209:9200 \
  -e discovery.type=single-node \
  -e xpack.security.enabled=false \
  -e xpack.security.http.ssl.enabled=false \
  -e "ES_JAVA_OPTS=-Xms512m -Xmx512m" \
  docker.elastic.co/elasticsearch/elasticsearch:9.0.0

# 等待就緒（各約 30–60 秒）
for port in 9208 9209; do
  for i in $(seq 1 30); do
    curl -sf "http://localhost:$port/_cluster/health" >/dev/null && echo "ES :$port ready" && break
    sleep 5
  done
done
```

**預期**：兩行 `ES :<port> ready`。等超過 150 秒沒好 → `podman logs elkdoctor-es8 | tail -30` 按〈疑難排解〉查。

> 與 docker-compose 版的差異：不設 `--ulimit memlock`（部分 podman 版本會拒絕，
> 而我們沒開 `bootstrap.memory_lock`，本來就不需要）。映像名必須寫完整 registry
> （`docker.elastic.co/...`），podman 不吃短名。

## 3. 啟動 Kibana（版本必須與所連的 ES 完全一致）

```bash
podman run -d --name elkdoctor-kibana8 -p 5601:5601 \
  -e ELASTICSEARCH_HOSTS=http://host.containers.internal:9208 \
  docker.elastic.co/kibana/kibana:8.14.3

podman run -d --name elkdoctor-kibana9 -p 5602:5601 \
  -e ELASTICSEARCH_HOSTS=http://host.containers.internal:9209 \
  docker.elastic.co/kibana/kibana:9.0.0

# 等待就緒（Kibana 慢，各約 1–2 分鐘）
for port in 5601 5602; do
  for i in $(seq 1 40); do
    curl -s "http://localhost:$port/api/status" | grep -q '"level":"available"' && echo "Kibana :$port ready" && break
    sleep 5
  done
done
```

**預期**：兩行 `Kibana :<port> ready`，瀏覽器開 `http://localhost:5601`（Dev Tools 在 Management → Dev Tools）。

> `host.containers.internal` 是 podman 容器連宿主機的慣用名（等同 docker 的
> `host.docker.internal`）。若 Kibana log 出現連不上 ES，改用
> `podman network create elkdoctor` 建共用網路、兩邊都加 `--network elkdoctor`，
> Kibana 的 `ELASTICSEARCH_HOSTS` 改成 `http://elkdoctor-es8:9200`。

## 4. 環境驗收（開始測試前必跑）

```bash
curl -s http://localhost:9208 | grep '"number"'   # 預期 8.14.3
curl -s http://localhost:9209 | grep '"number"'   # 預期 9.0.0
make build && ./elk-diagnostics version            # 預期印出工具版本
./elk-diagnostics check --host http://localhost:9208 --output text | head -5
```

**預期**：全新叢集的基準線是「30 ✅ ＋ 1 ⚠（單節點 master 單點，這是正確判定）＋ 0 ❓」。
若出現其他 ⚠／❌／❓，先停下來：這代表環境不乾淨或工具有問題，帶著輸出回報，不要直接開始造壓。

## 5. 完整測試流程

環境就緒後，照以下順序走（細節與指令都在引用的文件裡，不在此重複）：

1. **基準線**：`./collect.sh -h http://localhost:9208 -o bundle-baseline` →
   `./elk-diagnostics check --from-bundle bundle-baseline --output text`
2. **造壓**：配方見 [`docs/VERIFICATION.md`](./docs/VERIFICATION.md) §5（皆已驗證可乾淨復原）。
   **一次只做一個情境**；造壓指令貼 Kibana Dev Tools 執行。
3. **檢測**：每個情境都走客戶動線——`collect.sh` 採新 bundle → `--from-bundle --output text`
   現場判讀 → 同一 bundle 出 `--output html` 看交付視角。
4. **復原**：照配方復原後重跑，確認回到基準線。「復原後回綠」與「造壓後變紅」同等重要。
5. **降級驗證**：任挑一個 bundle，刪掉 `health_report.json` 再分析——預期 A 類全 ❓
   （措辭「bundle 缺少該端點資料」）、B/C 類照常判定、exit code 3。
6. **記錄**：任何「工具說的話與叢集實況不符」的觀察，記回 `docs/VERIFICATION.md`
   §1 的 bug 模式清單；驗證結論按 §7 的更新規則寫（只有刻意觸發並確認正確報出才算 ✅）。

## 6. 清理

```bash
podman rm -f elkdoctor-es8 elkdoctor-es9 elkdoctor-kibana8 elkdoctor-kibana9
podman network rm elkdoctor 2>/dev/null || true
```

## 7. 疑難排解

| 症狀 | 原因與處置 |
|---|---|
| ES 起了幾秒就退出，log 有 `vm.max_map_count [65530] is too low` | 回 §1b 設 sysctl（VM 重開會失效） |
| `podman run` 報 `ulimit`／`memlock` 相關錯誤 | 本指引的指令不帶 memlock；若你自行加了 `--ulimit`，拿掉 |
| 拉映像逾時或被拒 | 公司網路多半有 proxy／私有 registry。查 `podman machine ssh "env | grep -i proxy"`；必要時請使用者提供公司的 registry mirror，把映像名前綴換掉 |
| 埠被占用（`address already in use`） | `lsof -iTCP:9208 -sTCP:LISTEN` 找出占用者；不要改埠號硬繞——VERIFICATION 的配方與文件都假設這組埠 |
| Kibana 一直 `unavailable` | 先確認對應 ES 健康；再看 `podman logs elkdoctor-kibana8`。連線問題按 §3 的共用網路做法 |
| ES 回應極慢／OOM | `podman machine ls` 看 VM 記憶體；不足就 `podman machine stop` → `podman machine set --memory 8192` → `start`（VM 重開後記得重設 §1b 的 sysctl） |

## 8. Agent 行為守則

- 只操作 `elkdoctor-*` 開頭的容器；機器上其他容器一律不碰。
- 造壓一次一個情境、結束必復原；離開前跑 §6 清理（除非使用者說要留著環境）。
- 測試中發現工具缺陷：**只記錄、不順手修**——記下重現步驟與實際輸出，回報後由維護流程處理。
- 這份指引若與實際行為不符（版本、埠、參數），修正這份文件並說明原因，不要留下沉默的繞路。
