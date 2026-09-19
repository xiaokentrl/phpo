#!/usr/bin/env bash
# verify-signing-guard.sh —— 校验内嵌公钥与签名私钥匹配（硬红线 6）
#
# 背景：go:embed 的内嵌公钥在 build 期即固化进二进制。若发布时嵌入的仍是占位公钥
#   （或与签名私钥不匹配），客户端 VerifyPackage 会拒绝一切升级包，自升级形同虚设。
#   故发布签名前必须校验二者匹配。被签口径与 scripts/sign-release.sh / verifier.go 一致。
#
# 匹配 → exit 0；不匹配（多为占位未替换）→ 打印 ::error:: 并 exit 1。
#
# 用法: verify-signing-guard.sh <私钥PEM路径> [内嵌公钥文件路径]
#   内嵌公钥路径默认 internal/updater/signing/public.key
set -euo pipefail

KEY="${1:?用法: verify-signing-guard.sh <私钥PEM> [内嵌公钥路径]}"
PUB="${2:-internal/updater/signing/public.key}"
[ -f "$KEY" ] || { echo "私钥不存在: $KEY" >&2; exit 1; }
[ -f "$PUB" ] || { echo "内嵌公钥文件不存在: $PUB" >&2; exit 1; }

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
# 从私钥反推 base64 裸公钥（复用 sign-release.sh pubkey，口径逐字一致）
bash "$(dirname "$0")/sign-release.sh" pubkey "$KEY" "$tmp" >/dev/null

norm() { tr -d '[:space:]' < "$1"; }
if [ "$(norm "$tmp")" != "$(norm "$PUB")" ]; then
  echo "::error::内嵌的 $PUB 与签名私钥不匹配（疑占位公钥未替换）；请先用 sign-release.sh pubkey 导出真实公钥并提交后再发布（硬红线 6）"
  exit 1
fi
echo "✓ 内嵌公钥与签名私钥匹配（$PUB）"
