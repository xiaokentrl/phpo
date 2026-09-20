#!/usr/bin/env bash
# bump-version.sh：在「最后一个版本」基础上递增版本号，并同步到安装包版本，使每次出包可区分、可覆盖升级。
# 用法：scripts/bump-version.sh [patch|minor|major]   默认 patch（小版本号，如 0.1.0 → 0.1.1）
# 效果：改写 wails.json 的 productVersion（单一真实来源）+ 同步 build/linux/nfpm.yaml 的 version（deb/rpm 包内版本），并打印新版本。
set -euo pipefail
cd "$(dirname "$0")/.."

cur="$(bash scripts/version.sh)"
IFS='.' read -r maj min pat <<<"$cur"
case "${1:-patch}" in
  major) maj=$((maj + 1)); min=0; pat=0 ;;
  minor) min=$((min + 1)); pat=0 ;;
  *)     pat=$((pat + 1)) ;;
esac
new="$maj.$min.$pat"

tmp="$(mktemp)"
sed -E "s/(\"productVersion\"[[:space:]]*:[[:space:]]*\")[0-9][0-9.]*(\")/\1${new}\2/" wails.json >"$tmp"
mv "$tmp" wails.json
sed -E "s/^version:[[:space:]]*[0-9][0-9.]*/version: ${new}/" build/linux/nfpm.yaml >"$tmp"
mv "$tmp" build/linux/nfpm.yaml

echo "$new"
