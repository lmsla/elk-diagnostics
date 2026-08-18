package collector

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Options{Hosts: []string{srv.URL}})
	if err != nil {
		t.Fatalf("New() 失敗: %v", err)
	}
	return c
}

const versionBody = `{"cluster_name":"test","cluster_uuid":"cluster-uuid","version":{"number":"8.14.3"}}`

func TestGet_RetriesOnServerError(t *testing.T) {
	orig := retryDelay
	retryDelay = time.Millisecond
	defer func() { retryDelay = orig }()

	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	c.retries = 3

	b, err := c.get("/flaky")
	if err != nil {
		t.Fatalf("get() 失敗: %v", err)
	}
	if string(b) != `{"ok":true}` {
		t.Errorf("body = %q, want ok", b)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3（前 2 次 503 後第 3 次成功）", got)
	}
}

func TestGet_ExhaustsRetriesOnPersistentServerError(t *testing.T) {
	orig := retryDelay
	retryDelay = time.Millisecond
	defer func() { retryDelay = orig }()

	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	c.retries = 2

	_, err := c.get("/always-500")
	if err == nil {
		t.Fatal("want error，5xx 持續失敗應回傳錯誤")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3（retries=2 → 共 3 次嘗試）", got)
	}
}

func TestGet_DoesNotRetryOn4xx(t *testing.T) {
	orig := retryDelay
	retryDelay = time.Millisecond
	defer func() { retryDelay = orig }()

	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"reason":"no unassigned shard to explain"}}`))
	})
	c.retries = 3

	body, err := c.get("/_cluster/allocation/explain")
	if err == nil {
		t.Fatal("want error，4xx 屬永久失敗")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1（4xx 不重試）", got)
	}
	if len(body) == 0 {
		t.Error("4xx 仍應回傳 body（部分端點用 4xx 表達語意化非錯誤狀態，如 allocation/explain）")
	}
}

func TestGet_NoRetryOnFirstSuccess(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		atomic.AddInt32(&attempts, 1)
		w.Write([]byte(`{"ok":true}`))
	})
	c.retries = 5

	if _, err := c.get("/ok"); err != nil {
		t.Fatalf("get() 失敗: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1（首次即成功不應重試）", got)
	}
}
