#!/usr/bin/env bash
# gen-checksums.sh —— 发布产物 SHA256 清单（§11.1 checksums.txt）
#
# 作用：对 build/bin 下的发布产物逐计算 SHA256，生成标准 `<sha256>  <文件名>` 格式清单，
#       供用户/CI 用 `sha256sum -c checksums.txt` 校验下载完整性。
#
# 用法：
#   scripts/gen-checksums.sh                      # 默认对 build/bin 下发布产物出 build/bin/checksums.txt
#   scripts/gen-checksums.sh file1 file2 ...      # 对显式列出的文件出 ./checksums.txt
#   OUT=path/to/checksums.txt scripts/gen-checksums.sh
#
# 产物匹配（§11.1）：.exe .dmg .AppImage .deb .rpm .tar.gz（不含 .sig/checksums 自身）
set -euo pipefail

BIN_DIR="${BIN_DIR:-build/bin}"
ARTIFACT_RE='\.(exe|dmg|AppImage|deb|rpm|pkg\.tar\.zst|tar\.gz)$'

sha256_of() {
  # 优先 sha256sum（Linux/uutils），退化 shasum -a 256（macOS）
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# 收集待处理文件：显式参数优先，否则扫描 BIN_DIR
files=()
if [ "$#" -gt 0 ]; then
  files=("$@")
  out="${OUT:-./checksums.txt}"
else
  [ -d "$BIN_DIR" ] || { echo "目录不存在: $BIN_DIR" >&2; exit 1; }
  while IFS= read -r f; do
    base="$(basename "$f")"
    case "$base" in
      checksums.txt|*.sig) continue ;;
    esac
    printf '%s\n' "$base" | grep -Eq "$ARTIFACT_RE" && files+=("$f")
  done < <(find "$BIN_DIR" -maxdepth 1 -type f | sort)
  out="${OUT:-$BIN_DIR/checksums.txt}"
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "无发布产物可校验（先跑 task package 出包）" >&2
  exit 1
fi

: > "$out"
for f in "${files[@]}"; do
  printf '%s  %s\n' "$(sha256_of "$f")" "$(basename "$f")" >> "$out"
done

echo "✓ 已生成 $out（${#files[@]} 项）"
sed 's/^/    /' "$out"
