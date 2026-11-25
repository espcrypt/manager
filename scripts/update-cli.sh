#!/usr/bin/env bash
set -euo pipefail

REPO="espcrypt/manager"

bold=$'\033[1m'; dim=$'\033[2m'; green=$'\033[32m'; red=$'\033[31m'; reset=$'\033[0m'
say()  { printf '%s==>%s %s\n' "$bold" "$reset" "$*"; }
ok()   { printf '%s✓%s %s\n' "$green" "$reset" "$*"; }
die()  { printf '%s✗ %s%s\n' "$red" "$*" "$reset" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) asset_re='espcrypt-cli-[^"]*-macOS-universal\.tar\.gz' ;;
  Linux)
    case "$(uname -m)" in
      x86_64|amd64) asset_re='espcrypt-cli-[^"]*-linux-x64\.tar\.gz' ;;
      aarch64|arm64) asset_re='espcrypt-cli-[^"]*-linux-arm64\.tar\.gz' ;;
      *) die "unsupported CPU $(uname -m)" ;;
    esac ;;
  *) die "use the Windows release on Windows" ;;
esac

target="${ESPCRYPT_BIN:-$(command -v espcrypt || true)}"
[ -n "$target" ] || die "espcrypt is not installed (use install-macos.sh or install-linux.sh)"
target="$(cd "$(dirname "$target")" && pwd -P)/$(basename "$target")"
current="$("$target" version 2>/dev/null | awk '{print $2}' || true)"

token="${GITHUB_TOKEN:-}"
if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then token="$(gh auth token 2>/dev/null || true)"; fi
auth=()
[ -n "$token" ] && auth=(-H "Authorization: Bearer $token")

say "Checking for updates (installed: ${current:-unknown})"
rel_path="latest"
[ -n "${ESPCRYPT_VERSION:-}" ] && rel_path="tags/v${ESPCRYPT_VERSION#v}"
release="$(curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/releases/$rel_path")" \
  || die "cannot reach the release list (private repository? set GITHUB_TOKEN)"
tag="$(printf '%s' "$release" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
[ -n "$tag" ] || die "no release found"

if [ "${tag#v}" = "$current" ] && [ "${ESPCRYPT_FORCE:-0}" != "1" ]; then
  ok "espcrypt $current is up to date"
  exit 0
fi

url="$(printf '%s' "$release" | tr -d '\n' | grep -o "\"url\": *\"https://api.github.com/repos/[^\"]*/releases/assets/[0-9]*\"[^{}]*\"name\": *\"$asset_re\"" | head -1 | grep -o 'https://api.github.com/repos/[^"]*/releases/assets/[0-9]*' || true)"
[ -n "$url" ] || die "release $tag has no CLI for this platform"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
say "Downloading espcrypt $tag"
curl -fL --progress-bar "${auth[@]}" -H "Accept: application/octet-stream" -o "$tmp/cli.tar.gz" "$url"
tar -xzf "$tmp/cli.tar.gz" -C "$tmp"
chmod 755 "$tmp/espcrypt"
[ "$(uname -s)" = "Darwin" ] && xattr -c "$tmp/espcrypt" 2>/dev/null || true
"$tmp/espcrypt" version >/dev/null 2>&1 || die "the downloaded binary does not run on this system"

agent_was_running=0
if "$target" agent status 2>/dev/null | grep "running on" >/dev/null; then agent_was_running=1; fi

dir="$(dirname "$target")"
sudo=""
[ -w "$dir" ] || { say "Replacing $target needs your password"; sudo="sudo"; }
$sudo rm -f "$target"
$sudo install -m 755 "$tmp/espcrypt" "$target"
ok "espcrypt updated ${current:-?} → ${tag#v}"

if [ "$agent_was_running" = 1 ]; then
  printf '%sThe SSH agent keeps running the old version until you restart it: espcrypt agent stop && espcrypt agent start%s\n' "$dim" "$reset"
fi
