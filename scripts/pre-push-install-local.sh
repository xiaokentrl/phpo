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

if pgrep -x phpo >/dev/null 2>&1; then
  fail "close the running phpo application before pushing; the hook will replace its installed package"
fi

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

if [[ "${PHPO_PRE_PUSH_DRY_RUN:-}" == 1 ]]; then
  printf 'phpo pre-push dry run:\n'
  printf '  source: %s\n' "$head_commit"
  printf '  package: %s\n' "$deb"
  printf '  detected installation: Debian package phpo %s at /usr/bin/phpo\n' "$installed_version"
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

sudo -v || fail "administrator authorization is required to replace the installed package"
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
sudo apt-get install -y --reinstall "$deb"
[[ "$(dpkg-query -W -f='${Status}' phpo 2>/dev/null || true)" == "install ok installed" ]] \
  || fail "the newly built phpo package is not installed"
[[ -x /usr/bin/phpo ]] || fail "the installed package did not provide /usr/bin/phpo"
if (( had_user_data == 1 )); then
  [[ -d "$config_dir" ]] || fail "the existing phpo user data directory disappeared during package replacement: $config_dir"
fi

printf 'phpo pre-push: installed phpo %s; allowing push\n' "$version"
