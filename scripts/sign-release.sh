#!/usr/bin/env bash
# sign-release.sh —— 发布产物 Ed25519 签名（硬红线 6 / §11.1 .sig）
#
# 签名约定必须与 internal/updater/verifier.go 逐字一致，否则应用内验签失败：
#   signature = base64(Ed25519(privateKey, 小写sha256Hex字符串的字节))   —— 被签消息是「hex 字符串」本身（64 ASCII 字节），不是文件字节
#   公钥嵌入格式 = base64(StdEncoding, 32 字节裸 Ed25519 公钥)，落 internal/updater/signing/public.key
#
# 私钥绝不入库（.gitignore 已挡 *.key/*.pem）。真实签名/公证在持钥的发布 runner 上执行。
#
# 子命令：
#   genkey   <私钥路径>                 生成 Ed25519 私钥（已存在则报错，防覆盖）
#   pubkey   <私钥路径> <公钥输出路径>   从私钥导出 base64 裸公钥（用于替换嵌入占位公钥）
#   sign     [<产物...>]                对 build/bin 发布产物（或显式参数）逐个出 <artifact>.sig
#
# 环境变量：
#   PHPO_SIGNING_KEY   私钥 PEM 路径（sign 时必填；genkey/pubkey 用位置参数）
#   BIN_DIR            产物目录，默认 build/bin
set -euo pipefail

BIN_DIR="${BIN_DIR:-build/bin}"
ARTIFACT_RE='\.(exe|dmg|AppImage|deb|rpm|pkg\.tar\.zst|tar\.gz)$'

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

cmd="${1:-sign}"; shift || true
case "$cmd" in
  genkey)
    key="${1:?用法: sign-release.sh genkey <私钥路径>}"
    [ -e "$key" ] && { echo "私钥已存在，拒绝覆盖: $key" >&2; exit 1; }
    mkdir -p "$(dirname "$key")"
    openssl genpkey -algorithm ed25519 -out "$key"
    chmod 600 "$key"
    echo "✓ 已生成 Ed25519 私钥: $key（请加入 CI secret，切勿提交）"
    ;;
  pubkey)
    key="${1:?用法: sign-release.sh pubkey <私钥路径> <公钥输出路径>}"
    out="${2:?缺少公钥输出路径}"
    mkdir -p "$(dirname "$out")"
    # 私钥→SPKI DER→末尾 32 字节裸公钥→base64（与 verifier.go / embed.go 解析口径一致）
    openssl pkey -in "$key" -pubout -outform DER 2>/dev/null | tail -c 32 | base64 -w0 > "$out"
    echo "✓ 已导出公钥: $out"
    ;;
  sign)
    key="${PHPO_SIGNING_KEY:?签名需设置环境变量 PHPO_SIGNING_KEY 指向私钥 PEM}"
    [ -f "$key" ] || { echo "私钥不存在: $key" >&2; exit 1; }
    files=()
    if [ "$#" -gt 0 ]; then
      files=("$@")
    else
      [ -d "$BIN_DIR" ] || { echo "目录不存在: $BIN_DIR" >&2; exit 1; }
      while IFS= read -r f; do
        printf '%s\n' "$(basename "$f")" | grep -Eq "$ARTIFACT_RE" && files+=("$f")
      done < <(find "$BIN_DIR" -maxdepth 1 -type f | sort)
    fi
    [ "${#files[@]}" -eq 0 ] && { echo "无发布产物可签名" >&2; exit 1; }
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    for f in "${files[@]}"; do
      [ -f "$f" ] || continue                                    # 跳过 .app 等非普通文件
      h="$(sha256_of "$f")"
      printf '%s' "$h" > "$tmp"                       # 被签消息＝小写 hex 字符串，无尾换行
      openssl pkeyutl -sign -inkey "$key" -rawin -in "$tmp" | base64 -w0 > "$f.sig"
      echo "✓ 签名 $(basename "$f").sig"
    done
    ;;
  *)
    echo "未知子命令: $cmd（可用 genkey|pubkey|sign）" >&2
    exit 2
    ;;
esac
