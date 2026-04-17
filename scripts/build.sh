#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${repo_root}/dist"

rm -rf "${dist_dir}"
mkdir -p "${dist_dir}"

(
	cd "${repo_root}"
	GOOS=js GOARCH=wasm go build -trimpath -o "${dist_dir}/t4play.wasm" ./cmd/playground
)
