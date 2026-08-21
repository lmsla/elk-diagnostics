package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func collectorModuleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../collectors")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCollectorModulesAreExecutablePOSIXShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	files, err := filepath.Glob(filepath.Join(collectorModuleDir(t), "*.sh"))
	if err != nil || len(files) != 5 {
		t.Fatalf("採集模組數量 = %d, err=%v", len(files), err)
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(file) != "http-common.sh" && info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s 應可執行", file)
		}
		if out, err := exec.Command(sh, "-n", file).CombinedOutput(); err != nil {
			t.Errorf("%s 語法錯誤: %v\n%s", file, err, out)
		}
	}
}

func TestCollectScriptRunsOptionalAPIModules(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_name":"test","version":{"number":"8.14.3"}}`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	script := filepath.Join(tmp, "collect.sh")
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "bundle")
	cmd := exec.Command(sh, script,
		"--services", "es,kibana,logstash",
		"-h", srv.URL,
		"--kibana-url", srv.URL,
		"--logstash-url", srv.URL,
		"--logstash-sample-interval", "0",
		"-o", out,
	)
	cmd.Env = append(os.Environ(), "COLLECT_MODULE_DIR="+collectorModuleDir(t))
	log, err := cmd.CombinedOutput()
	t.Logf("collect output:\n%s", log)
	if err != nil {
		t.Fatalf("選配 API 模組採集失敗: %v\n%s", err, log)
	}
	for _, file := range []string{
		"elasticsearch/version.json",
		"kibana/default/status.json",
		"kibana/default/stats.json",
		"kibana/default/task_manager_health.json",
		"kibana/default/alerting_health.json",
		"logstash/default/node_info.json",
		"logstash/default/node_stats.json",
		"logstash/default/hot_threads.txt",
		"logstash/default/root.json",
		"logstash/default/health_report.json",
		"logstash/default/node_plugins.json",
		"logstash/default/node_pipelines.json",
		"logstash/default/pipelines_sample_1.json",
	} {
		if _, err := os.Stat(filepath.Join(out, file)); err != nil {
			t.Errorf("缺少 %s: %v", file, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(out, "_manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Services []string `json:"services"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(manifest.Services, ","); got != "elasticsearch,kibana,logstash" {
		t.Errorf("services = %q", got)
	}
}

func TestKibanaCollectorRejectsPasswordPromptWithoutTTY(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	tmp := t.TempDir()
	cmd := exec.Command(sh, filepath.Join(collectorModuleDir(t), "kibana.sh"),
		"--url", "http://127.0.0.1:1", "--output", filepath.Join(tmp, "kibana"))
	cmd.Env = append(os.Environ(),
		"KIBANA_USERNAME=elastic",
		"KIBANA_PASSWORD_FILE=",
		"KIBANA_API_KEY=",
	)
	cmd.Stdin = strings.NewReader("should-not-be-used\n")
	log, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(log), "不是互動式 Terminal") {
		t.Fatalf("非互動模式應要求 password file 或 API key: err=%v output=%s", err, log)
	}
	if strings.Contains(string(log), "should-not-be-used") {
		t.Fatal("密碼不應出現在錯誤輸出")
	}
}

func TestCollectScriptRejectsUnknownService(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	script := filepath.Join(t.TempDir(), "collect.sh")
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(sh, script, "--services", "beats").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "不支援的採集模組") {
		t.Fatalf("未知模組應被拒絕: err=%v out=%s", err, out)
	}
}

func TestCollectScriptRunsHostOnlyWithoutES(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	tmp := t.TempDir()
	script := filepath.Join(tmp, "collect.sh")
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(tmp, "collectors")
	if err := os.Mkdir(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHost := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) out="$2"; shift 2 ;;
    *) exit 2 ;;
  esac
done
mkdir -p "$out/os"
printf 'hostname=test-host\n' > "$out/os/baseline.txt"
printf 'os/baseline.txt OK\n' > "$out/_status.txt"
`
	if err := os.WriteFile(filepath.Join(modules, "host.sh"), []byte(fakeHost), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "bundle")
	cmd := exec.Command(sh, script, "--services", "host", "--host-id", "local", "-o", out)
	cmd.Env = append(os.Environ(), "COLLECT_MODULE_DIR="+modules)
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("host-only 採集失敗: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(out, "host", "local", "os", "baseline.txt")); err != nil {
		t.Fatalf("Host 證據未落檔: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "_manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"services": ["host"]`) {
		t.Fatalf("manifest 未記錄 host 模組: %s", b)
	}
}

func TestSSHModuleStreamsHostCollectorWithoutPasswordVault(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	tmp := t.TempDir()
	fakeSSH := filepath.Join(tmp, "ssh")
	fake := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    *) shift; break ;;
  esac
done
exec sh -c "$1"
`
	if err := os.WriteFile(fakeSSH, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(tmp, "hosts.conf")
	if err := os.WriteFile(hosts, []byte("node-a|tester@example.invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "host")
	cmd := exec.Command(sh, filepath.Join(collectorModuleDir(t), "ssh.sh"),
		"--hosts-file", hosts,
		"--host-collector", filepath.Join(collectorModuleDir(t), "host.sh"),
		"--output", out,
	)
	cmd.Env = append(os.Environ(), "SSH_BIN="+fakeSSH)
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SSH 模組自我檢查失敗: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(out, "node-a", "os", "baseline.txt")); err != nil {
		t.Fatalf("遠端 Host 證據未落檔: %v", err)
	}
}
