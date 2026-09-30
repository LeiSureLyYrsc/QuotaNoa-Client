#!/usr/bin/env sh
# QuotaNoa Client 管理脚本（Linux / macOS / Git Bash）
#
# 用法：
#   ./scripts/quotanoa-client.sh [命令] [参数]
#
# 不带参数直接运行会进入交互控制台（编号菜单 + 命令提示符，q 退出）。
#
# 命令：
#   interactive      进入交互控制台（等效别名：i / menu）
#   init [--force]   生成默认配置文件（--force 覆盖已存在文件）
#   check            校验配置并打印摘要（不联网）
#   patch            备份后补齐配置缺失项并写入 config_version
#   add <类型> …     添加实例/账号：add cpa|volc|wb|qoder [参数]（写入前自动备份）
#   list [类型]      列出实例/账号（省略类型列出全部）
#   rm <类型> …      按名称删除（remove 别名；写入前自动备份）
#   version          打印客户端与协议版本
#   run [参数]       前台运行（Ctrl-C 退出）
#   start [参数]     后台运行并写日志
#   stop             停止后台进程
#   restart [参数]   重启后台进程
#   status           查看运行状态（并打印配置摘要）
#   logs [行数]      查看后台日志（默认 40 行）
#   help             显示本帮助
#
# 可用环境变量：
#   QUOTANOA_BIN     可执行文件路径（默认 <仓库根>/quotanoa-client）
#   QUOTANOA_CONFIG  配置文件路径（默认 <仓库根>/config.json）
#   QUOTANOA_LOG     后台日志路径（默认 <仓库根>/quotanoa-client.log）
#   QUOTANOA_PID     PID 文件路径（默认 <仓库根>/quotanoa-client.pid）

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

BIN=${QUOTANOA_BIN:-"$ROOT_DIR/quotanoa-client"}
CONFIG=${QUOTANOA_CONFIG:-"$ROOT_DIR/config.json"}
LOG=${QUOTANOA_LOG:-"$ROOT_DIR/quotanoa-client.log"}
PIDFILE=${QUOTANOA_PID:-"$ROOT_DIR/quotanoa-client.pid"}

die() {
    echo "错误：$*" >&2
    exit 1
}

require_bin() {
    [ -x "$BIN" ] || die "找不到可执行文件：$BIN
请先构建：go build -o quotanoa-client ./cmd/quotanoa-client
或用 QUOTANOA_BIN 指定路径。"
}

read_pid() {
    [ -f "$PIDFILE" ] || return 1
    pid=$(cat "$PIDFILE" 2>/dev/null || true)
    [ -n "$pid" ] || return 1
    printf '%s' "$pid"
}

is_running() {
    pid=$(read_pid) || return 1
    kill -0 "$pid" 2>/dev/null
}

cmd_init() {
    require_bin
    mkdir -p "$(dirname -- "$CONFIG")"
    case "${1:-}" in
        --force | -f) "$BIN" config init --out "$CONFIG" --force ;;
        *) "$BIN" config init --out "$CONFIG" ;;
    esac
}

cmd_check() {
    require_bin
    "$BIN" config check --config "$CONFIG"
}

cmd_patch() {
    require_bin
    "$BIN" config patch --config "$CONFIG"
}

cmd_add() {
    require_bin
    "$BIN" config add "$@" --config "$CONFIG"
}

cmd_list() {
    require_bin
    "$BIN" config list "$@" --config "$CONFIG"
}

cmd_rm() {
    require_bin
    "$BIN" config remove "$@" --config "$CONFIG"
}

cmd_version() {
    require_bin
    "$BIN" version
}

cmd_run() {
    require_bin
    exec "$BIN" run --config "$CONFIG" "$@"
}

cmd_start() {
    require_bin
    if is_running; then
        echo "已在运行（PID $(read_pid)），未重复启动。"
        return 0
    fi
    mkdir -p "$(dirname -- "$LOG")"
    nohup "$BIN" run --config "$CONFIG" "$@" >>"$LOG" 2>&1 &
    pid=$!
    printf '%s' "$pid" >"$PIDFILE"
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
        echo "已启动（PID $pid）。日志：$LOG"
    else
        rm -f "$PIDFILE"
        die "启动失败，请查看日志：$LOG"
    fi
}

cmd_stop() {
    if ! is_running; then
        rm -f "$PIDFILE"
        echo "未在运行。"
        return 0
    fi
    pid=$(read_pid)
    kill "$pid" 2>/dev/null || true
    i=0
    while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 20 ]; do
        sleep 0.5
        i=$((i + 1))
    done
    if kill -0 "$pid" 2>/dev/null; then
        kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$PIDFILE"
    echo "已停止（PID $pid）。"
}

cmd_restart() {
    cmd_stop
    cmd_start "$@"
}

cmd_status() {
    if is_running; then
        echo "运行中：PID $(read_pid)"
    else
        echo "未运行。"
    fi
    echo "可执行文件：$BIN"
    echo "配置文件：$CONFIG"
    if [ -x "$BIN" ] && [ -f "$CONFIG" ]; then
        "$BIN" config check --config "$CONFIG" 2>&1 || true
    fi
}

cmd_logs() {
    [ -f "$LOG" ] || die "日志不存在：$LOG"
    tail -n "${1:-40}" "$LOG"
}

usage() {
    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
}

#: 交互菜单编号 → 子命令。
menu_action() {
    case "$1" in
        1) printf 'init' ;;
        2) printf 'check' ;;
        3) printf 'patch' ;;
        4) printf 'start' ;;
        5) printf 'stop' ;;
        6) printf 'restart' ;;
        7) printf 'status' ;;
        8) printf 'logs' ;;
        9) printf 'version' ;;
        10) printf 'add' ;;
        11) printf 'list' ;;
        12) printf 'rm' ;;
        *) printf '' ;;
    esac
}

#: 执行一行交互输入（编号或“子命令 参数”）。退出码 100 表示退出交互。
run_line() {
    line=$1
    case "$line" in
        q | quit | exit) return 100 ;;
    esac
    mapped=$(menu_action "$line")
    [ -n "$mapped" ] && line=$mapped
    set -f
    # shellcheck disable=SC2086
    set -- $line
    set +f
    [ $# -gt 0 ] || return 0
    action=$1
    shift
    case "$action" in
        init) cmd_init "$@" ;;
        check) cmd_check ;;
        patch) cmd_patch ;;
        add) cmd_add "$@" ;;
        list) cmd_list "$@" ;;
        rm | remove) cmd_rm "$@" ;;
        version) cmd_version ;;
        run) cmd_run "$@" ;;
        start) cmd_start "$@" ;;
        stop) cmd_stop ;;
        restart) cmd_restart "$@" ;;
        status) cmd_status ;;
        logs) cmd_logs "$@" ;;
        help) usage ;;
        *) echo "未知命令：$action（输入 help 查看用法）" >&2 ;;
    esac
    return 0
}

interactive() {
    cat <<'MENU'
QuotaNoa Client 交互控制台（输入编号或命令，q 退出）
  1) init      生成默认配置       6) restart   重启
  2) check     校验配置           7) status    状态
  3) patch     备份并修补配置     8) logs      查看日志（logs 100）
  4) start     后台启动           9) version   版本
  5) stop      停止               q) 退出
  10) add      添加实例：add cpa --name Home --base-url URL --key K
  11) list     列出实例：list [cpa|volc|wb|qoder]
  12) rm       删除实例：rm cpa --name Home
MENU
    while :; do
        printf 'quotanoa> '
        if ! read -r line; then
            printf '\n'
            break
        fi
        line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
        [ -n "$line" ] || continue
        # 子进程执行，隔离 die/exec，避免单条命令失败中断交互。
        set +e
        ( run_line "$line" )
        status=$?
        set -e
        if [ "$status" -eq 100 ]; then
            break
        fi
    done
}

main() {
    cmd=${1:-interactive}
    if [ $# -gt 0 ]; then
        shift
    fi
    case "$cmd" in
        init) cmd_init "$@" ;;
        check) cmd_check ;;
        patch) cmd_patch ;;
        add) cmd_add "$@" ;;
        list) cmd_list "$@" ;;
        rm | remove) cmd_rm "$@" ;;
        version) cmd_version ;;
        run) cmd_run "$@" ;;
        start) cmd_start "$@" ;;
        stop) cmd_stop ;;
        restart) cmd_restart "$@" ;;
        status) cmd_status ;;
        logs) cmd_logs "$@" ;;
        interactive | i | menu) interactive ;;
        help | -h | --help) usage ;;
        *) die "未知命令：$cmd（运行 help 查看用法）" ;;
    esac
}

main "$@"
