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
	"unicode"
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

func TestCollectScriptDefinesTTYColorPolicy(t *testing.T) {

	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[ -t 1 ]",
		"NO_COLOR",
		"COLOR_GREEN=$(printf '\\033[32m')",
		"COLOR_YELLOW=$(printf '\\033[33m')",
		"COLOR_RED=$(printf '\\033[31m')",
		"print_colored()",
		"table_value_width()",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("產生的採集腳本缺少顏色控制 %q", want)
		}
	}
	if strings.Contains(s, "| wc -L") {
		t.Error("表格欄寬不應依賴平台差異較大的 wc -L")
	}
	for _, want := range []string{
		"load_config()",
		"collect.conf",
		"--config FILE",
		"命令列參數優先",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("產生的採集腳本缺少設定檔支援 %q", want)
		}
	}
	for _, forbidden := range []string{"source ", ". ./", "eval ", "eval\t"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("collect.conf 不應被當成 shell 程式執行: %q", forbidden)
		}
	}
}

func TestCollectScriptLoadsAdjacentConfigAndAllowsCLIOutputOverride(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(tmp, "expected-es-nodes.txt"), []byte("node-a|10.99.1.11\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "kibana-instances.conf"), []byte("kb-01|https://kibana.example.local:5601\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "logstash-instances.conf"), []byte("ls-01|http://logstash.example.local:9600\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `services=es,kibana,logstash
es_url=https://10.99.1.123:9200
output=bundle-from-config
expected_es_nodes_file=expected-es-nodes.txt
kibana_list=kibana-instances.conf
logstash_list=logstash-instances.conf
logstash_sample_interval=0
redact_index_names=false
`
	if err := os.WriteFile(filepath.Join(tmp, "collect.conf"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCurl := filepath.Join(bin, "curl")
	fake := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w|-K|--max-time) shift 2 ;;
    -q|-sS) shift ;;
    *) shift ;;
  esac
done
printf '{"cluster_name":"config-test","version":{"number":"8.14.3"}}' > "$out"
printf '200'
`
	if err := os.WriteFile(fakeCurl, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(tmp, "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(withoutEnv(os.Environ(), "COLLECT_CONFIG"),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"COLLECT_MODULE_DIR="+collectorModuleDir(t),
	)

	cmd := exec.Command(sh, script)
	cmd.Dir = workDir
	cmd.Env = env
	log, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("自動載入同目錄 collect.conf 失敗: %v\n%s", err, log)
	}
	configOut := filepath.Join(tmp, "bundle-from-config")
	if _, err := os.Stat(filepath.Join(configOut, "_manifest.json")); err != nil {
		t.Fatalf("設定檔的相對 output 未生效: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(configOut, "_expected_es_nodes.txt")); err != nil {
		t.Fatalf("設定檔的相對 expected node 路徑未生效: %v", err)
	}
	for _, dir := range []string{"kibana/kb-01", "logstash/ls-01"} {
		if info, err := os.Stat(filepath.Join(configOut, dir)); err != nil || !info.IsDir() {
			t.Fatalf("設定檔的相對 instance 清單未生效（%s）: %v", dir, err)
		}
	}

	overrideOut := filepath.Join(tmp, "bundle-from-cli")
	cmd = exec.Command(sh, script, "--output", overrideOut)
	cmd.Dir = workDir
	cmd.Env = env
	log, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("命令列 output 覆寫設定檔失敗: %v\n%s", err, log)
	}
	if _, err := os.Stat(filepath.Join(overrideOut, "_manifest.json")); err != nil {
		t.Fatalf("命令列 output 未覆寫設定檔: %v\n%s", err, log)
	}
}

func TestCollectScriptRejectsUnknownConfigKey(t *testing.T) {
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
	config := filepath.Join(tmp, "bad.conf")
	if err := os.WriteFile(config, []byte("not_a_supported_key=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := exec.Command(sh, script, "--config", config).CombinedOutput()
	if err == nil || !strings.Contains(string(log), "不支援的 key") {
		t.Fatalf("未知設定 key 應被拒絕: err=%v output=%s", err, log)
	}
}

func terminalDisplayWidth(s string) int {
	width := 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Han), r == '／':
			width += 2
		default:
			width++
		}
	}
	return width
}

func assertSummaryTableRowsHaveMatchingWidths(t *testing.T, output string) {
	t.Helper()
	borderWidth := 0
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			borderWidth = terminalDisplayWidth(line)
		case borderWidth > 0 && strings.HasPrefix(line, "|"):
			if got := terminalDisplayWidth(line); got != borderWidth {
				t.Errorf("摘要表格欄寬不一致：預期 %d、實際 %d：%q", borderWidth, got, line)
			}
		}
	}
}

func TestCollectScriptRunsMultipleServiceInstances(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_name":"test","version":{"number":"8.14.3"},"status":{"overall":{"level":"available"}}}`))
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
	kibanaList := filepath.Join(tmp, "kibana-instances.conf")
	logstashList := filepath.Join(tmp, "logstash-instances.conf")
	for _, item := range []struct {
		path string
		body string
	}{
		{kibanaList, "kb-01|" + srv.URL + "\nkb-02|" + srv.URL + "\n"},
		{logstashList, "ls-01|" + srv.URL + "\nls-02|" + srv.URL + "\n"},
	} {
		if err := os.WriteFile(item.path, []byte(item.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(tmp, "bundle")
	cmd := exec.Command(sh, script,
		"--services", "es,kibana,logstash",
		"-h", srv.URL,
		"--kibana-list", kibanaList,
		"--logstash-list", logstashList,
		"--logstash-sample-interval", "0",
		"-o", out,
	)
	cmd.Env = append(os.Environ(), "COLLECT_MODULE_DIR="+collectorModuleDir(t))
	log, err := cmd.CombinedOutput()
	t.Logf("multi-instance collect output:\n%s", log)
	if err != nil {
		t.Fatalf("多 instance 採集失敗: %v\n%s", err, log)
	}
	for _, dir := range []string{
		"kibana/kb-01", "kibana/kb-02", "logstash/ls-01", "logstash/ls-02",
	} {
		if info, err := os.Stat(filepath.Join(out, dir)); err != nil || !info.IsDir() {
			t.Errorf("缺少多 instance 目錄 %s: %v", dir, err)
		}
	}
	output := string(log)
	if strings.Contains(output, "\x1b[") {
		t.Error("非互動式測試輸出不應包含 ANSI 顏色控制碼")
	}
	assertSummaryTableRowsHaveMatchingWidths(t, output)
	for _, want := range []string{
		"[Kibana] kb-01", "[Kibana] kb-02",
		"[Logstash] ls-01", "[Logstash] ls-02",
		"採集摘要", "Elasticsearch 端點摘要", "ES 節點盤點", "選配服務摘要",
		"服務", "目標數", "核心 API 可用", "部分端點失敗", "核心 API 失敗",
		"+--------------------------+--------+--------+--------------------------+",
		"+--------------------------+--------+--------+--------+",
		"+--------------+-----------+-------------------+--------------------+--------------------+",
		"| 項目", "| Elasticsearch API", "| Kibana", "| Logstash",
		"Kibana", "Logstash",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("採集輸出缺少 %q", want)
		}
	}
	for _, unwanted := range []string{
		"子採集器失敗",
		"下一步（在可執行 elk-diagnostics 的機器上）",
		"註：非 2xx 不一定代表有問題",
	} {
		if strings.Contains(output, unwanted) {
			t.Errorf("採集輸出不應包含 %q", unwanted)
		}
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
