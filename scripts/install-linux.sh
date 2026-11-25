#!/usr/bin/env bash
set -euo pipefail

REPO="espcrypt/manager"
BIN_DIR="${ESPCRYPT_BIN_DIR:-/usr/local/bin}"

bold=$'\033[1m'; dim=$'\033[2m'; green=$'\033[32m'; red=$'\033[31m'; reset=$'\033[0m'
say()  { printf '%s==>%s %s\n' "$bold" "$reset" "$*"; }
ok()   { printf '%s✓%s %s\n' "$green" "$reset" "$*"; }
die()  { printf '%s✗ %s%s\n' "$red" "$*" "$reset" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "this installer is for Linux (use install-macos.sh on macOS)"
case "$(uname -m)" in
  x86_64|amd64) arch=x64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m) (x64 and arm64 are supported)" ;;
esac

sudo=""
[ "$(id -u)" -eq 0 ] || sudo="sudo"

if ! { ldconfig -p 2>/dev/null || /sbin/ldconfig -p 2>/dev/null || true; } | grep libpcsclite.so.1 >/dev/null; then
  say "Installing the PC/SC library"
  if command -v apt-get >/dev/null 2>&1; then
    $sudo apt-get update -qq && $sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq libpcsclite1 curl ca-certificates
  elif command -v dnf >/dev/null 2>&1; then
    $sudo dnf install -y -q pcsc-lite-libs curl
  elif command -v pacman >/dev/null 2>&1; then
    $sudo pacman -Sy --needed --noconfirm pcsclite curl
  elif command -v zypper >/dev/null 2>&1; then
    $sudo zypper --non-interactive install libpcsclite1 curl
  elif command -v apk >/dev/null 2>&1; then
    $sudo apk add pcsc-lite-libs curl
  else
    die "unknown package manager: install the PC/SC library (libpcsclite) yourself"
  fi
fi

token="${GITHUB_TOKEN:-}"
if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then token="$(gh auth token 2>/dev/null || true)"; fi
auth=()
[ -n "$token" ] && auth=(-H "Authorization: Bearer $token")

api() { curl -fsSL "${auth[@]}" -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/$1"; }

say "Looking up the latest release"
if [ -n "${ESPCRYPT_VERSION:-}" ]; then
  release="$(api "releases/tags/v${ESPCRYPT_VERSION#v}")" || die "release v${ESPCRYPT_VERSION#v} not found"
else
  release="$(api releases/latest)" || die "cannot reach the release list (private repository? set GITHUB_TOKEN)"
fi
tag="$(printf '%s' "$release" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')"
[ -n "$tag" ] || die "no release found"

name_re="espcrypt-cli-[^\"]*-linux-$arch\.tar\.gz"
url="$(printf '%s' "$release" | tr -d '\n' | grep -o "\"url\": *\"https://api.github.com/repos/[^\"]*/releases/assets/[0-9]*\"[^{}]*\"name\": *\"$name_re\"" | head -1 | grep -o 'https://api.github.com/repos/[^"]*/releases/assets/[0-9]*' || true)"
[ -n "$url" ] || die "release $tag has no Linux $arch CLI"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
say "Downloading espcrypt $tag ($arch)"
curl -fL --progress-bar "${auth[@]}" -H "Accept: application/octet-stream" -o "$tmp/cli.tar.gz" "$url"
tar -xzf "$tmp/cli.tar.gz" -C "$tmp"

if mkdir -p "$BIN_DIR" 2>/dev/null && [ -w "$BIN_DIR" ]; then
  rm -f "$BIN_DIR/espcrypt"
  install -m 755 "$tmp/espcrypt" "$BIN_DIR/espcrypt"
else
  $sudo mkdir -p "$BIN_DIR"
  $sudo rm -f "$BIN_DIR/espcrypt"
  $sudo install -m 755 "$tmp/espcrypt" "$BIN_DIR/espcrypt"
fi
ok "espcrypt $tag installed in $BIN_DIR"

if [ "${ESPCRYPT_NO_DEPS:-0}" != "1" ]; then
  say "Setting up the smart card service, serial access and VeraCrypt"
  if (exec </dev/tty) 2>/dev/null; then
    "$BIN_DIR/espcrypt" setup </dev/tty || printf '%sSetup did not finish; run "espcrypt setup" later.%s\n' "$dim" "$reset"
  else
    "$BIN_DIR/espcrypt" setup || printf '%sSetup did not finish; run "espcrypt setup" later.%s\n' "$dim" "$reset"
  fi
fi

case ":$PATH:" in *":$BIN_DIR:"*) ;; *) printf '%sAdd %s to your PATH to use espcrypt.%s\n' "$dim" "$BIN_DIR" "$reset" ;; esac
printf '\n%sDone.%s Run %spicohsm%s for the interactive CLI.\n' "$bold" "$reset" "$bold" "$reset"
