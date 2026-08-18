# Fielddata

這份筆記整理 Fielddata、`text`、`keyword`、`doc_values` 之間的關係，以及如何解讀健檢報告中的 `fielddata_memory`。

## Q1：Fielddata 是什麼？

Fielddata 是 Elasticsearch 為 `text` 欄位建立的記憶體內資料結構，讓分析後的詞元可以被用於：

- 排序
- 聚合，例如 `terms aggregation`
- Script 透過 `doc['field']` 取值

全文搜尋使用的倒排索引比較適合「詞 → 文件」查詢；排序與聚合需要「文件 → 詞值」的查詢方式。Fielddata 就是為了支援後者。

## Q2：Fielddata 預設會開啟嗎？

不會。`text` 欄位的 Fielddata 預設關閉。

```json
{
  "message": {
    "type": "text",
    "fielddata": true
  }
}
```

只有明確設定 `"fielddata": true`，`text` 欄位才可透過 Fielddata 做排序、聚合或標準的 Script 取值。

## Q3：Fielddata 會不會把原文切成詞？

不是 Fielddata 負責切詞，而是 `text` 欄位的 analyzer 在索引時分析原文。

```text
New York office
→ new、york、office
```

Fielddata 只是把這些分析後的詞元及其文件關聯，在查詢需要時載入 JVM heap。

## Q4：為什麼要載入 JVM heap？

因為排序、聚合和 Script 需要快速查詢「某份文件有哪些詞值」。Elasticsearch 會建立適合這種查詢方式的記憶體結構，放入 Fielddata cache。

Fielddata 通常是需要時才載入，不是每筆資料寫入時就立即複製一份到 heap。

## Q5：沒有 Fielddata，`text` 還能搜尋嗎？

可以。`text` 仍然可以做全文搜尋，例如 `match` 和 `match_phrase`。

但若要對 `text` 做標準的排序、聚合或使用 `doc['field']` 的 Script，通常會收到：

```text
Fielddata is disabled on text fields by default
```

## Q6：`keyword` 也能排序、聚合和 Script，為什麼還需要 Fielddata？

`keyword` 適合完整值的精確處理，例如：

- `status=ERROR` 的統計
- 依 `service.name` 排序
- 依 `host.name` 聚合

`keyword` 使用預設開啟的 `doc_values`，不需要 Fielddata。

Fielddata 的特殊用途是對分析後的詞元做處理。例如要統計 `New York office` 中的 `new`、`york`、`office`，`keyword` 只會把整段文字視為一個完整值，不能直接取代 Fielddata。

## Q7：實務上應該怎麼設計欄位？

通常使用 multi-field，同時保留全文搜尋與精確聚合能力：

```json
{
  "message": {
    "type": "text",
    "fields": {
      "keyword": {
        "type": "keyword"
      }
    }
  }
}
```

- `message`：全文搜尋
- `message.keyword`：精確比對、排序、聚合

新設計通常優先使用 `keyword` 或 `text` + `keyword`，不要為了方便而開啟大型 `text` 欄位的 Fielddata。

## Q8：什麼是高基數欄位？為什麼會造成記憶體壓力？

高基數代表欄位有很多不重複值：

- 低基數：`INFO`、`WARN`、`ERROR`
- 高基數：`user_id`、完整 URL、`request_id`、`trace_id`

若對高基數 `text` 欄位啟用 Fielddata，Elasticsearch 需要在 heap 保存大量不同詞元及其文件關聯，可能造成：

- JVM heap 上升
- GC 增加
- 第一次載入時查詢延遲尖峰
- Fielddata cache eviction 增加
- circuit breaker 拒絕查詢
- 極端情況下 `OutOfMemoryError`

即使改用 `keyword`，高基數聚合仍可能消耗查詢記憶體；只是通常不會以 Fielddata cache 的形式呈現。

## Q9：`fielddata_memory` 報告項目怎麼解讀？

目前專案使用：

```http
GET /_nodes/stats/indices/fielddata
```

主要讀取：

- `memory_size_in_bytes`：每個節點目前 Fielddata cache 使用的 heap 記憶體
- `evictions`：節點啟動後累積的 Fielddata cache 淘汰次數

判讀原則：

- 記憶體大不代表單獨故障，需搭配 JVM Heap、查詢延遲和 breaker 訊息。
- 單次 `evictions > 0` 先標示為藍色 `INFO／需觀察`，不代表當下必然故障；cache 淘汰可能只是正常的快取管理。
- `evictions` 是累積值；要判斷是否持續發生，應比較兩次以上採集結果。
- 目前沒有可直接套用於所有叢集的官方固定 eviction 門檻；只有在 eviction 持續增加，且同時看到 JVM Heap、GC、查詢延遲或 circuit breaker 壓力時，才提升為 `warning`／`critical` 的調查層級。
- 沒有經過真機基準線，不應自行設定固定的 memory 門檻。

## Q10：`_cat/fielddata` 和 Nodes Stats 有什麼差別？

V2.3 參考採集器使用：

```http
GET /_cat/fielddata?v&s=size:desc
```

它以「節點 + 欄位」列出目前 Fielddata 使用量，適合人工查看；`size` 只是排序欄位。

目前專案使用 Nodes Stats，以「節點」彙總 Fielddata 記憶體及 eviction，較適合程式採集、完整性檢查和診斷分析。Elastic 官方也建議應用程式優先使用 Nodes Stats API。

## Q11：Fielddata 什麼時候算合理？

以下情況可能合理：

- 舊 index 已使用 `text` mapping，且短期無法改成 `keyword` 或重建 index。
- 確實需要對分析後詞元做聚合。
- 欄位很小且基數低，能接受 heap 成本。

否則應優先調整 mapping，使用 `keyword` 或 multi-field，而不是直接開啟 Fielddata。

## Q12：舊 index 如何啟用 Fielddata？

如果既有欄位已經是 `text`，可以直接使用 Update Mapping API，不必先重建 index：

```http
PUT my-index-000001/_mapping
{
  "properties": {
    "message": {
      "type": "text",
      "fielddata": true
    }
  }
}
```

提交內容應保留該欄位原本的 mapping 設定，例如 analyzer；執行者需要該 index 的 `manage` 權限。

確認設定：

```http
GET my-index-000001/_mapping/field/message
```

啟用後，Fielddata 通常在第一次排序、聚合或 Script 需要時才載入 JVM heap，不是立即把所有資料載入記憶體。

## Q13：什麼情況仍需要重建 index？

以下情況通常不能直接修改既有欄位，需建立新 index 並重新索引：

- 將既有 `text` 改成 `keyword`。
- 修改 analyzer。
- 讓既有文件補上新的 `.keyword` multi-field 值。

新增 multi-field 後，新寫入的文件會有該欄位；更新 mapping 前已存在的文件仍需更新或重新索引，才會產生對應值。

## 官方參考

- [Text field 與 Fielddata](https://www.elastic.co/guide/en/elasticsearch/reference/current/text.html)
- [Field data cache settings](https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-fielddata.html)
- [Nodes stats API](https://www.elastic.co/guide/en/elasticsearch/reference/8.19/cluster-nodes-stats.html)
- [Doc values](https://www.elastic.co/guide/en/elasticsearch/reference/8.19/doc-values.html)
- [Keyword field type](https://www.elastic.co/guide/en/elasticsearch/reference/8.19/keyword.html)
- [Update Mapping API](https://www.elastic.co/guide/en/elasticsearch/reference/8.19/indices-put-mapping.html)
