#!/bin/sh
# Linux Host OS 唯讀子採集器。刻意不收完整 process argv、服務設定檔與日誌。

set -eu

OUT=""
usage() { echo "用法: host.sh --output <host 目錄>" >&2; }
while [ $# -gt 0 ]; do
    case "$1" in
        --output) OUT="${2:-}"; shift 2 ;;
        --help) usage; exit 0 ;;
        *) echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done
[ -n "$OUT" ] || { usage; exit 2; }

umask 077
OS_OUT="$OUT/os"
STATUS="$OUT/_status.txt"
mkdir -p "$OS_OUT"
: > "$STATUS"
record() { printf '%s %s\n' "$1" "$2" >> "$STATUS"; }

{
    echo "hostname=$(hostname 2>/dev/null || echo unknown)"
    echo "os=$(uname -s 2>/dev/null || echo unknown)"
    grep -m1 '^PRETTY_NAME=' /etc/os-release 2>/dev/null || true
    echo "kernel=$(uname -r 2>/dev/null || echo unknown)"
    grep -m1 '^MemTotal:' /proc/meminfo 2>/dev/null || true
    uptime 2>/dev/null || true
} > "$OS_OUT/baseline.txt"
record 'os/baseline.txt' OK

if command -v vmstat >/dev/null 2>&1 && vmstat 1 3 > "$OS_OUT/vmstat.txt" 2>&1; then
    record 'os/vmstat.txt' OK
else
    rm -f "$OS_OUT/vmstat.txt"
    record 'os/vmstat.txt' NOT_SUPPORTED
fi

if df -hTl > "$OS_OUT/filesystems.txt" 2>&1; then
    record 'os/filesystems.txt' OK
else
    rm -f "$OS_OUT/filesystems.txt"
    record 'os/filesystems.txt' FAILED
fi

if command -v iostat >/dev/null 2>&1 && iostat -xz 1 3 > "$OS_OUT/io.txt" 2>&1; then
    record 'os/io.txt' OK
elif command -v lsblk >/dev/null 2>&1 && lsblk > "$OS_OUT/io.txt" 2>&1; then
    record 'os/io.txt' TOPOLOGY_ONLY
else
    rm -f "$OS_OUT/io.txt"
    record 'os/io.txt' NOT_SUPPORTED
fi

if command -v ip >/dev/null 2>&1 && ip -s link > "$OS_OUT/network.txt" 2>&1; then
    record 'os/network.txt' OK
elif command -v ifconfig >/dev/null 2>&1 && ifconfig > "$OS_OUT/network.txt" 2>&1; then
    record 'os/network.txt' OK
else
    rm -f "$OS_OUT/network.txt"
    record 'os/network.txt' NOT_SUPPORTED
fi

{
    echo "vm.swappiness=$(cat /proc/sys/vm/swappiness 2>/dev/null || echo unknown)"
    echo "vm.max_map_count=$(cat /proc/sys/vm/max_map_count 2>/dev/null || echo unknown)"
    echo "transparent_hugepage=$(cat /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null || echo unknown)"
    if command -v swapon >/dev/null 2>&1; then
        if swapon --show 2>/dev/null | grep -q .; then echo 'swap_enabled=yes'; else echo 'swap_enabled=no'; fi
    else
        echo 'swap_enabled=unknown'
    fi
} > "$OS_OUT/kernel_settings.txt"
record 'os/kernel_settings.txt' OK

if command -v dmesg >/dev/null 2>&1; then
    if dmesg -T 2>/dev/null | tail -n 300 > "$OS_OUT/dmesg.txt" && [ -s "$OS_OUT/dmesg.txt" ]; then
        record 'os/dmesg.txt' OK
    else
        rm -f "$OS_OUT/dmesg.txt"
        record 'os/dmesg.txt' SKIPPED_PERMISSION
    fi
fi
