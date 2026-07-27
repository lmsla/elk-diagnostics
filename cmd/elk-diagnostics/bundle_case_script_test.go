package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const bundleCaseScript = "../../dev/phase0/bundle-case.sh"
const faultScenariosScript = "../../dev/phase0/fault-scenarios.sh"

func TestBundleCaseScriptSyntaxAndExecutable(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("本機未安裝 bash")
	}
	info, err := os.Stat(bundleCaseScript)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("bundle-case.sh 必須具備執行權限")
	}
	if out, err := exec.Command(bash, "-n", bundleCaseScript).CombinedOutput(); err != nil {
		t.Fatalf("bash -n 失敗: %v\n%s", err, out)
	}
}

func TestFaultScenariosP08KeepsSingleNodeClusterGreen(t *testing.T) {
	b, err := os.ReadFile(faultScenariosScript)
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	triggerStart := strings.Index(script, "p08_trigger() {")
	verifyStart := strings.Index(script, "p08_verify() {")
	restoreStart := strings.Index(script, "p08_restore() {")
	if triggerStart < 0 || verifyStart <= triggerStart || restoreStart <= verifyStart {
		t.Fatal("找不到完整的 P08 trigger/verify 區段")
	}
	trigger := script[triggerStart:verifyStart]
	verify := script[verifyStart:restoreStart]
	if !strings.Contains(trigger, `"number_of_replicas":0`) {
		t.Fatal("P08 必須設定 number_of_replicas=0，避免單節點測試混入 unassigned replica")
	}
	if !strings.Contains(verify, "'green' 'index_health'") {
		t.Fatal("P08 verify 必須確認測試 index 維持 green")
	}
}

func TestFaultScenariosNonAllocationCasesKeepSingleNodeIndicesGreen(t *testing.T) {
	b, err := os.ReadFile(faultScenariosScript)
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	cases := []struct {
		id               string
		minReplicaZeroes int
	}{
		{id: "P05", minReplicaZeroes: 2},
		{id: "P09", minReplicaZeroes: 1},
		{id: "P10", minReplicaZeroes: 1},
		{id: "P12", minReplicaZeroes: 2},
		{id: "P15", minReplicaZeroes: 1},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			prefix := strings.ToLower(tc.id)
			triggerStart := strings.Index(script, prefix+"_trigger() {")
			verifyStart := strings.Index(script, prefix+"_verify() {")
			restoreStart := strings.Index(script, prefix+"_restore() {")
			if triggerStart < 0 || verifyStart <= triggerStart || restoreStart <= verifyStart {
				t.Fatalf("找不到完整的 %s trigger/verify 區段", tc.id)
			}
			trigger := script[triggerStart:verifyStart]
			verify := script[verifyStart:restoreStart]
			if count := strings.Count(trigger, `"number_of_replicas":0`); count < tc.minReplicaZeroes {
				t.Fatalf("%s 至少需要 %d 個 number_of_replicas=0，實際只有 %d 個",
					tc.id, tc.minReplicaZeroes, count)
			}
			if !strings.Contains(verify, "'green' 'index_health'") &&
				!strings.Contains(verify, "'green' 'cluster_health'") {
				t.Fatalf("%s verify 必須確認測試資料未讓單節點叢集變成 yellow", tc.id)
			}
		})
	}
}

func TestBundleCaseScriptStagesAndRunCleanup(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("本機未安裝 bash")
	}
	tmp := t.TempDir()
	fault := filepath.Join(tmp, "fault.sh")
	collect := filepath.Join(tmp, "collect.sh")
	state := filepath.Join(tmp, "fault-state")
	logFile := filepath.Join(tmp, "calls.log")
	ca := filepath.Join(tmp, "ca.crt")
	evidence := filepath.Join(tmp, "evidence")

	writeExecutable(t, fault, `#!/usr/bin/env bash
set -euo pipefail
printf '%s %s\n' "$1" "$2" >> "$FAKE_LOG"
case "$1:$2" in
  baseline:verify) [[ ! -s "$FAKE_STATE" ]] ;;
  P*:trigger) printf '%s\n' "$1" > "$FAKE_STATE" ;;
  P*:verify) grep -qx "$1" "$FAKE_STATE" ;;
  P*:restore) : > "$FAKE_STATE" ;;
  *) exit 10 ;;
esac
`)
	writeExecutable(t, collect, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${FAKE_COLLECT_FAIL:-0}" == 1 ]]; then
  exit 23
fi
out=''
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$out" ]]
mkdir -p "$out"
printf '{}\n' > "$out/_manifest.json"
printf 'version.json 200\n' > "$out/_status.txt"
printf '{}\n' > "$out/version.json"
printf '%s\n' "$out" >> "$FAKE_LOG"
`)
	if err := os.WriteFile(ca, []byte("test-ca"), 0o600); err != nil {
		t.Fatal(err)
	}

	baseEnv := append(os.Environ(),
		"ES_URL=https://localhost:9208",
		"ES_USER=elastic",
		"ES_PASSWORD=test-only",
		"CA_CERT="+ca,
		"EVIDENCE_ROOT="+evidence,
		"FAULT_CMD="+fault,
		"COLLECT_SH="+collect,
		"FAKE_STATE="+state,
		"FAKE_LOG="+logFile,
	)
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(bash, append([]string{bundleCaseScript}, args...)...)
		cmd.Env = baseEnv
		return cmd.CombinedOutput()
	}

	if out, err := run("P00", "collect"); err != nil {
		t.Fatalf("P00 collect 失敗: %v\n%s", err, out)
	}
	assertFile(t, filepath.Join(evidence, "P00", "bundle", "_manifest.json"))

	if out, err := run("P01", "trigger"); err != nil {
		t.Fatalf("P01 trigger 失敗: %v\n%s", err, out)
	}
	assertContent(t, filepath.Join(evidence, ".bundle-case-active"), "P01")
	assertContent(t, state, "P01")

	if out, err := run("P01", "collect"); err != nil {
		t.Fatalf("P01 collect 失敗: %v\n%s", err, out)
	}
	assertFile(t, filepath.Join(evidence, "P01-fault", "bundle", "_status.txt"))

	if out, err := run("P01", "restore"); err != nil {
		t.Fatalf("P01 restore 失敗: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(evidence, ".bundle-case-active")); !os.IsNotExist(err) {
		t.Fatalf("restore 後 active marker 仍存在: %v", err)
	}
	if out, err := run("P01", "collect-post"); err != nil {
		t.Fatalf("P01 collect-post 失敗: %v\n%s", err, out)
	}
	assertFile(t, filepath.Join(evidence, "post-P01", "bundle", "_status.txt"))

	if out, err := run("P02", "run"); err != nil {
		t.Fatalf("P02 run 失敗: %v\n%s", err, out)
	}
	assertFile(t, filepath.Join(evidence, "P02-fault", "bundle", "_status.txt"))
	assertFile(t, filepath.Join(evidence, "post-P02", "bundle", "_status.txt"))

	failEnv := append([]string{}, baseEnv...)
	failEnv = append(failEnv, "FAKE_COLLECT_FAIL=1")
	cmd := exec.Command(bash, bundleCaseScript, "P03", "run")
	cmd.Env = failEnv
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("collect 故障時 run 應失敗\n%s", out)
	}
	if b, err := os.ReadFile(state); err != nil {
		t.Fatal(err)
	} else if strings.TrimSpace(string(b)) != "" {
		t.Fatalf("run 失敗後未自動還原，state=%q", b)
	}
	if _, err := os.Stat(filepath.Join(evidence, ".bundle-case-active")); !os.IsNotExist(err) {
		t.Fatalf("run 失敗後 active marker 仍存在: %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("缺少檔案 %s: %v", path, err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
