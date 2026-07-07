cmake_minimum_required(VERSION 3.16)

project(local_asr_service LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

option(LOCAL_ASR_USE_SHERPA_ONNX "Build local-asr-service with sherpa-onnx C API SenseVoice engine" OFF)

add_executable(local-asr-service
  src/main.cpp
)

target_compile_options(local-asr-service PRIVATE
  -Wall
  -Wextra
  -Wpedantic
)

if(LOCAL_ASR_USE_SHERPA_ONNX)
  if(NOT SHERPA_ONNX_INCLUDE_DIR)
    message(FATAL_ERROR "SHERPA_ONNX_INCLUDE_DIR is required when LOCAL_ASR_USE_SHERPA_ONNX=ON")
  endif()
  if(NOT SHERPA_ONNX_LIBRARY)
    message(FATAL_ERROR "SHERPA_ONNX_LIBRARY is required when LOCAL_ASR_USE_SHERPA_ONNX=ON")
  endif()

  target_compile_definitions(local-asr-service PRIVATE LOCAL_ASR_ENABLE_SHERPA_ONNX=1)
  target_include_directories(local-asr-service PRIVATE "${SHERPA_ONNX_INCLUDE_DIR}")
  target_link_libraries(local-asr-service PRIVATE "${SHERPA_ONNX_LIBRARY}")
endif()
