#!/usr/bin/env bash
# 本脚本用于在 Windows + MSYS2 环境下编译 local-asr-service。
set -euo pipefail

cd "$(dirname "$0")"

mkdir -p .tmp build
export HOME="$PWD/.tmp/home"
export TMPDIR="$PWD/.tmp"
mkdir -p "$HOME" "$TMPDIR"

cmake -S . -B build
cmake --build build -j2
