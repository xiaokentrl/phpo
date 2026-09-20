#!/usr/bin/env bash
# version.sh：打印当前应用版本（单一真实来源 = wails.json 的 productVersion）
set -euo pipefail
cd "$(dirname "$0")/.."
grep -oE '"productVersion"[^0-9]*[0-9][0-9.]*' wails.json | grep -oE '[0-9][0-9.]*'
