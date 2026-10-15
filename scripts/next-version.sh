#!/usr/bin/env bash
# next-version.sh：打印这次要发的版本号。
#
# 干什么：稳定版就照抄 wails.json 的 productVersion；预览版在它后面接 -beta.N，
#         N 由本地已有的同版本 beta 标签推出来（有 v0.1.118-beta.2 就给 0.1.118-beta.3）。
# 为什么要在脚本里算：手工敲后缀最容易发重复（标签已存在时 CI 会因「标签指向别的提交」判红），
#                     而版本号本身不许改——注入二进制的那份仍以 wails.json 为准。
#
# 用法：bash scripts/next-version.sh [stable|preview]   默认 stable
set -euo pipefail
cd "$(dirname "$0")/.."

base="$(bash scripts/version.sh)"
[ -n "$base" ] || { echo "取不到版本号（wails.json 的 productVersion 是空的）" >&2; exit 1; }

case "${1:-stable}" in
  stable) printf '%s\n' "$base" ;;
  preview)
    n=1
    while git rev-parse -q --verify "refs/tags/v$base-beta.$n" >/dev/null; do n=$((n + 1)); done
    printf '%s-beta.%d\n' "$base" "$n"
    ;;
  *) echo "只认 stable / preview，收到的是：$1" >&2; exit 2 ;;
esac
