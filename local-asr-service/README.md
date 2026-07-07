# Local ASR Service

本目录是 RK3588 本地 ASR 服务的最小版骨架，目标是把模型推理从 Go 会议主服务中拆出来。

当前版本先完成：

- 接收 `pcm_s16le_16000_mono` 原始音频。
- 按能量 VAD 做简单切段。
- 支持三种引擎：
  - `LOCAL_ASR_MOCK_TEXT`：链路测试。
  - `SENSEVOICE_COMMAND`：外部命令行调用。
  - `LOCAL_ASR_ENGINE=sherpa`：直接调用 sherpa-onnx C API。
- Python 开发服务支持可选 CAM++ 发言人 embedding 与在线聚类，返回 `speaker_1 / speaker_2 / ...`。
- 返回与 Go 侧 `meeting-asr-v1` 对齐的转写事件。

RK3588 正式部署时，建议使用 sherpa-onnx C API / RKNN Runtime 模式，避免每个语音段都启动外部命令。

## 编译

```bash
cd local-asr-service
cmake -S . -B build
cmake --build build -j
```

启用 sherpa-onnx C API：

```bash
cmake -S . -B build \
  -DLOCAL_ASR_USE_SHERPA_ONNX=ON \
  -DSHERPA_ONNX_INCLUDE_DIR=/opt/sherpa-onnx/include \
  -DSHERPA_ONNX_LIBRARY=/opt/sherpa-onnx/lib/libsherpa-onnx-c-api.so
cmake --build build -j
```

`SHERPA_ONNX_INCLUDE_DIR` 需要能找到：

```text
sherpa-onnx/c-api/c-api.h
```

## 启动

仅测试链路：

```bash
LOCAL_ASR_MOCK_TEXT="这是本地 ASR 测试文本" ./build/local-asr-service --port 9001
```

接真实 SenseVoice 命令时：

```bash
SENSEVOICE_COMMAND='sherpa-onnx-offline --tokens ./tokens.txt --sense-voice-model ./sense-voice-small.onnx --num-threads 4 {wav}' \
./build/local-asr-service --port 9001
```

`{wav}` 会被服务替换成当前语音段的临时 wav 文件路径。不同 sherpa-onnx 版本的命令参数可能不一样，实际部署时以目标版本为准。

接 sherpa-onnx C API 时：

```bash
LOCAL_ASR_ENGINE=sherpa \
SENSEVOICE_MODEL=/models/sense-voice-small/model.int8.rknn \
SENSEVOICE_TOKENS=/models/sense-voice-small/tokens.txt \
SENSEVOICE_PROVIDER=cpu \
SENSEVOICE_LANGUAGE=zh \
SENSEVOICE_NUM_THREADS=4 \
SENSEVOICE_USE_ITN=true \
SENSEVOICE_ENABLE_PUNCTUATION=true \
./build/local-asr-service --port 9001
```

如果使用 ONNX CPU 模型，`SENSEVOICE_MODEL` 填 `.onnx`。如果使用 RKNN 模型，模型文件和 provider 参数以当前 sherpa-onnx RKNN 版本要求为准。

## Python SenseVoice + CAM++ 开发服务

Windows/Ubuntu 快速验证时可使用：

```powershell
cd local-asr-service
..\.venv\Scripts\python.exe .\sensevoice_server.py --port 9001
```

默认只启用 SenseVoice，CAM++ 未配置时会回退为 `speaker_1`。

接 FunASR CAM++ 模型：

```powershell
$env:CAMPP_MODEL="iic/speech_campplus_sv_zh-cn_16k-common"
..\.venv\Scripts\python.exe .\sensevoice_server.py --port 9001
```

如果使用外部 CAM++ 推理命令：

```powershell
$env:CAMPP_COMMAND="python campp_embed.py --wav {wav}"
..\.venv\Scripts\python.exe .\sensevoice_server.py --port 9001
```

`CAMPP_COMMAND` 需要输出 JSON：

```json
{"embedding":[0.1,0.2,0.3]}
```

也可以直接输出：

```json
{"speakerLabel":"speaker_2"}
```

可调参数：

```env
SENSEVOICE_USE_ITN=true
SENSEVOICE_ENABLE_PUNCTUATION=true
LOCAL_ASR_ENABLE_SPEAKER_DIARIZATION=true
LOCAL_ASR_VAD_THRESHOLD=120
LOCAL_ASR_VAD_PEAK_THRESHOLD=900
LOCAL_ASR_VAD_NOISE_RATIO=3.0
LOCAL_ASR_END_SILENCE_MS=750
LOCAL_ASR_MIN_SPEECH_MS=500
LOCAL_ASR_MAX_SEGMENT_MS=12000
LOCAL_ASR_PRE_SPEECH_MS=300
LOCAL_ASR_NORMALIZE_PEAK=8000
LOCAL_ASR_MAX_GAIN=4
CAMPP_SPEAKER_THRESHOLD=0.40
CAMPP_SPEAKER_STICKY_THRESHOLD=0.28
CAMPP_MIN_SPEAKER_MS=1600
```

## HTTP API

健康检查：

```bash
curl http://127.0.0.1:9001/health
```

开始会话：

```bash
curl -X POST http://127.0.0.1:9001/v1/sessions/m_demo/start
```

发送音频：

```bash
curl -X POST --data-binary @sample.pcm http://127.0.0.1:9001/v1/sessions/m_demo/audio
```

强制刷新当前缓冲：

```bash
curl -X POST http://127.0.0.1:9001/v1/sessions/m_demo/flush
```

结束会话：

```bash
curl -X POST http://127.0.0.1:9001/v1/sessions/m_demo/stop
```

返回事件示例：

```json
{
  "ok": true,
  "events": [
    {
      "type": "transcript",
      "contractVersion": "meeting-asr-v1",
      "meetingId": "m_demo",
      "speakerLabel": "speaker_1",
      "text": "这是本地 ASR 测试文本",
      "isFinal": true,
      "confidence": 0.9,
      "provider": "sensevoice",
      "startedAtMs": 0,
      "endedAtMs": 1800
    }
  ]
}
```
