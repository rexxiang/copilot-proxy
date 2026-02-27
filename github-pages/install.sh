#!/bin/sh

set -eu

REPO="rexxiang/copilot-proxy"
BIN_NAME="copilot-proxy"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${os}" in
linux|darwin) ;;
*)
  echo "Unsupported OS: ${os}" >&2
  exit 1
  ;;
esac

arch="$(uname -m)"
case "${arch}" in
x86_64|amd64) arch="amd64" ;;
arm64|aarch64) arch="arm64" ;;
*)
  echo "Unsupported architecture: ${arch}" >&2
  exit 1
  ;;
esac

latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
tag="${latest_url##*/}"
if [ -z "${tag}" ] || [ "${tag}" = "latest" ]; then
  echo "Failed to fetch latest release tag" >&2
  exit 1
fi

asset="${BIN_NAME}_${tag}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
binary_path="${BIN_NAME}_${tag}_${os}_${arch}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT INT TERM

echo "Downloading ${url}"
curl -fL "${url}" -o "${tmp_dir}/${asset}"
tar -xzf "${tmp_dir}/${asset}" -C "${tmp_dir}"

if [ -w "${INSTALL_DIR}" ]; then
  install -m 0755 "${tmp_dir}/${binary_path}" "${INSTALL_DIR}/${BIN_NAME}"
else
  sudo install -m 0755 "${tmp_dir}/${binary_path}" "${INSTALL_DIR}/${BIN_NAME}"
fi

echo "Installed ${BIN_NAME} to ${INSTALL_DIR}/${BIN_NAME}"
