package collector

import (
	"encoding/json"
	"strconv"
)

type ThreadPoolRow struct {
	Node      string
	Name      string
	Active    int
	Queue     int
	Rejected  int
	Completed int
}

// ThreadPool 取 GET /_cat/thread_pool（rejected/completed 為自節點啟動起的累積值）。
func (c *Client) ThreadPool() ([]ThreadPoolRow, error) {
	b, err := c.get(EpCatThreadPool)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]ThreadPoolRow, 0, len(raw))
	for _, m := range raw {
		out = append(out, ThreadPoolRow{
			Node:      m["node_name"],
			Name:      m["name"],
			Active:    atoi(m["active"]),
			Queue:     atoi(m["queue"]),
			Rejected:  atoi(m["rejected"]),
			Completed: atoi(m["completed"]),
		})
	}
	return out, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
