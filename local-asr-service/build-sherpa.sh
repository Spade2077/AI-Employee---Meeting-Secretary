#!/usr/bin/env bash
# 本脚本用于在 Linux/RK3588 上编译启用 sherpa-onnx C API 的 local-asr-service。
set -euo pipefail

cd "$(dirname "$0")"

: "${SHERPA_ONNX_INCLUDE_DIR:?请设置 SHERPA_ONNX_INCLUDE_DIR，例如 /opt/sherpa-onnx/include}"
: "${SHERPA_ONNX_LIBRARY:?请设置 SHERPA_ONNX_LIBRARY，例如 /opt/sherpa-onnx/lib/libsherpa-onnx-c-api.so}"

cmake -S . -B build-sherpa \
  -DLOCAL_ASR_USE_SHERPA_ONNX=ON \
  -DSHERPA_ONNX_INCLUDE_DIR="$SHERPA_ONNX_INCLUDE_DIR" \
  -DSHERPA_ONNX_LIBRARY="$SHERPA_ONNX_LIBRARY"

cmake --build build-sherpa -j"$(nproc)"
