#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(git rev-parse --show-toplevel)"
repo_root="$(realpath -e -- "$repo_root")"
cd "$repo_root"

fail() {
  printf 'phpo pre-push: %s\n' "$*" >&2
  exit 1
}

for cmd in dpkg apt-get dpkg-deb pgrep sudo realpath wails3 pnpm go git; do
  command -v "$cmd" >/dev/null 2>&1 || fail "required command not found: $cmd"
done

[[ "$(uname -s)" == Linux ]] || fail "automatic local install is supported only on Linux"
[[ "$(dpkg --print-architecture)" == amd64 ]] || fail "local deb install currently requires amd64"
[[ -f /etc/debian_version ]] || fail "automatic local install requires Debian or Ubuntu"
[[ "$(dpkg-query -W -f='${Status}' phpo 2>/dev/null || true)" == "install ok installed" ]] \
  || fail "phpo is not installed as a Debian package; refusing to install beside another install type"
installed_version="$(dpkg-query -W -f='${Version}' phpo)"
installed_files="$(dpkg-query -L phpo)"
grep -Fxq /usr/bin/phpo <<<"$installed_files" \
  || fail "the installed phpo package does not own /usr/bin/phpo; refusing to install beside an unknown location"

head_commit="$(git rev-parse HEAD)"
push_has_current_head=0
while read -r local_ref local_sha remote_ref remote_sha; do
  [[ -n "${local_ref:-}" && -n "${local_sha:-}" ]] || continue
  [[ "$local_sha" =~ ^0+$ ]] && continue
  pushed_commit="$(git rev-parse --verify "${local_sha}^{commit}" 2>/dev/null)" \
    || fail "cannot resolve pushed ref $local_ref to a commit"
  [[ "$pushed_commit" == "$head_commit" ]] \
    || fail "push includes $local_ref, which is not the checked-out HEAD; install check would test the wrong source"
  push_has_current_head=1
done

(( push_has_current_head == 1 )) || {
  printf '%s\n' 'phpo pre-push: no commit at the checked-out HEAD is being pushed; skipping local install'
  exit 0
}

config_root="${XDG_CONFIG_HOME:-$HOME/.config}"
[[ "$config_root" == /* ]] || fail "XDG_CONFIG_HOME must be an absolute path"
config_root="$(realpath -m -- "$config_root")"
config_dir="$config_root/phpo"
if [[ -e "$config_dir" && ! -d "$config_dir" ]]; then
  fail "configuration path exists but is not a directory: $config_dir"
fi

version="$(bash scripts/version.sh)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.-]+)?$ ]] \
  || fail "could not read a valid application version from wails.json"
deb="$repo_root/build/bin/phpo_${version}_amd64.deb"

if pgrep -x phpo >/dev/null 2>&1; then
  running_state="will be stopped before install and relaunched after"
else
  running_state="not running"
fi

if [[ "${PHPO_PRE_PUSH_DRY_RUN:-}" == 1 ]]; then
  printf 'phpo pre-push dry run:\n'
  printf '  source: %s\n' "$head_commit"
  printf '  package: %s\n' "$deb"
  printf '  detected installation: Debian package phpo %s at /usr/bin/phpo\n' "$installed_version"
  printf '  running phpo instance: %s\n' "$running_state"
  if [[ -d "$config_dir" ]]; then
    printf '  preserve existing user data: %s\n' "$config_dir"
  else
    printf '  user data directory does not exist yet: %s\n' "$config_dir"
  fi
  printf '  replace the installed package in place and block push on failure\n'
  exit 0
fi

git diff --quiet HEAD -- || fail "tracked files have uncommitted changes; commit them before pushing"
git diff --cached --quiet HEAD -- || fail "staged changes are not part of HEAD; commit them before pushing"

while IFS= read -r path; do
  case "$path" in
    *.go|frontend/*.vue|frontend/*.ts|frontend/*.tsx|frontend/*.js|frontend/*.mjs|frontend/*.css|frontend/*.html|frontend/*.json|frontend/*.yaml|frontend/*.yml|frontend/src/*)
      fail "untracked source file could affect the build: $path"
      ;;
  esac
done < <(git ls-files --others --exclude-standard)

# phpo 还跑着就替用户关掉（放在所有快速失败检查之后：树不干净等情况下不动用户的应用）。
# 先 TERM 给它 10 秒自己退，退不掉再 KILL 兜底；都拦不住才挡 push。
# 应用没有信号处理器，TERM 即硬退出——sqlite 落盘安全，但队列里在跑的任务会被打断，这是本机验收流的既定代价。
stopped_running=0
if pgrep -x phpo >/dev/null 2>&1; then
  printf '%s\n' 'phpo pre-push: stopping the running phpo application'
  stopped_running=1
  pkill -x -TERM phpo || true
  stopped=0
  for _ in $(seq 1 50); do
    if ! pgrep -x phpo >/dev/null 2>&1; then stopped=1; break; fi
    sleep 0.2
  done
  if (( stopped == 0 )); then
    printf '%s\n' 'phpo pre-push: phpo did not exit on SIGTERM; sending SIGKILL'
    pkill -x -KILL phpo || true
    sleep 0.5
    pgrep -x phpo >/dev/null 2>&1 \
      && fail "could not stop the running phpo application; close it manually and push again"
  fi
fi

# 免密优先：sudoers NOPASSWD 规则放行 root 所有的 /usr/local/bin/phpo-pre-push-install
# （脚本内部只装本仓库 build/bin 里版本最新的 phpo deb，kentrl 不可写）。
# 没配规则的环境回退到交互式输密码——路径不变，机器之间行为一致。
trap 'git restore --source="$head_commit" --worktree -- frontend/dist/index.html' EXIT

printf '%s\n' 'phpo pre-push: building the local Debian package'
PATH="$(go env GOPATH)/bin:$PATH" wails3 task package:local
[[ -f "$deb" ]] || fail "package task did not produce $deb"
[[ "$(dpkg-deb -f "$deb" Package)" == "phpo" ]] \
  || fail "built package identity does not match the existing phpo installation"
[[ "$(dpkg-deb -f "$deb" Version)" == "$version" ]] \
  || fail "built package version does not match wails.json ($version)"

had_user_data=0
if [[ -d "$config_dir" ]]; then
  had_user_data=1
fi

printf 'phpo pre-push: replacing Debian package phpo %s in place\n' "$installed_version"
# 先用 -l 区分「规则没配」和「规则在但安装失败」：前者才回落交互式输密码，
# 后者（如 dpkg 锁被别的进程占用）原样把 sudo 的报错摆出来，不再误报免密规则缺失。
if sudo -n -l /usr/local/bin/phpo-pre-push-install >/dev/null 2>&1; then
  sudo -n /usr/local/bin/phpo-pre-push-install \
    || fail "package replacement failed; see the sudo error above (dpkg lock held? retry when the other process finishes)"
else
  printf '%s\n' 'phpo pre-push: no passwordless rule found, falling back to interactive sudo'
  sudo apt-get install -y --reinstall "$deb" \
    || fail "package replacement failed; configure the NOPASSWD rule (see /etc/sudoers.d/phpo-pre-push) or push from a terminal"
fi
[[ "$(dpkg-query -W -f='${Status}' phpo 2>/dev/null || true)" == "install ok installed" ]] \
  || fail "the newly built phpo package is not installed"
[[ -x /usr/bin/phpo ]] || fail "the installed package did not provide /usr/bin/phpo"
if (( had_user_data == 1 )); then
  [[ -d "$config_dir" ]] || fail "the existing phpo user data directory disappeared during package replacement: $config_dir"
fi

printf 'phpo pre-push: installed phpo %s; allowing push\n' "$version"

# 装完把替用户关掉的那个 phpo 拉起来：只在我们主动关过的情况下才拉（本来就没跑的不越权启动）。
# 尽力而为：拉起失败只提醒、不挡 push——包已经装好，push 该放行；图形会话不在（CI 等）同样只提醒。
if (( stopped_running == 1 )); then
  printf '%s\n' 'phpo pre-push: relaunching the newly installed phpo'
  if [[ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]]; then
    setsid nohup /usr/bin/phpo >/dev/null 2>&1 &
    relaunched=0
    for _ in $(seq 1 15); do
      if pgrep -x phpo >/dev/null 2>&1; then relaunched=1; break; fi
      sleep 0.2
    done
    if (( relaunched == 1 )); then
      printf '%s\n' 'phpo pre-push: phpo is back up'
    else
      printf '%s\n' 'phpo pre-push: could not confirm phpo started; run /usr/bin/phpo manually if needed'
    fi
  else
    printf '%s\n' 'phpo pre-push: no graphical session detected; start phpo manually if needed'
  fi
fi
