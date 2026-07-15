package collector

import "encoding/json"

// WritePoolRow：write thread pool 的大小與即時狀態（size 由 allocated_processors 衍生）。
type WritePoolRow struct {
	Node     string
	Size     int
	Active   int
	Queue    int
	Rejected int
}

// WritePool 取 GET /_cat/thread_pool/write（含 size，用於 write-bottleneck 因果鏈）。
func (c *Client) WritePool() ([]WritePoolRow, error) {
	b, err := c.get(EpCatThreadPoolWrite)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]WritePoolRow, 0, len(raw))
	for _, m := range raw {
		out = append(out, WritePoolRow{
			Node:     m["node_name"],
			Size:     atoi(m["size"]),
			Active:   atoi(m["active"]),
			Queue:    atoi(m["queue"]),
			Rejected: atoi(m["rejected"]),
		})
	}
	return out, nil
}
