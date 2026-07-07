# 本地 ASR 接口契约

本文档固定 Go 会议核心服务与本地 ASR 服务之间的边界。后续从火山 ASR 切到 SenseVoice-Small、Zipformer 或双路 ASR 时，业务层只消费统一事件，不直接依赖具体模型。

## 协议版本

```text
meeting-asr-v1
```

Go 侧定义位置：

```text
internal/stt/contract.go
```

## 标准音频格式

第一版统一使用：

```text
pcm_s16le_16000_mono
```

含义：

- 16kHz
- 单声道
- 16-bit signed little-endian PCM

浏览器麦克风、Go 后端、本地 ASR 服务都先按这个格式对齐。

## 标准转写事件

本地 ASR 服务最终要返回以下语义：

```json
{
  "type": "transcript",
  "contractVersion": "meeting-asr-v1",
  "meetingId": "m_xxx",
  "speakerLabel": "speaker_1",
  "text": "明天上午王磊测试 Ubuntu 环境",
  "isFinal": true,
  "confidence": 0.92,
  "provider": "sensevoice",
  "startedAtMs": 1200,
  "endedAtMs": 4300
}
```

字段说明：

| 字段 | 说明 |
|---|---|
| `meetingId` | 当前会议 ID |
| `speakerLabel` | 发言人标签；未接 CAM++ 前默认 `speaker_1` |
| `text` | 识别文本 |
| `isFinal` | 是否最终结果；SenseVoice 分段模式第一版只返回 `true` |
| `confidence` | 置信度；没有置信度时可先填 `0` 或估计值 |
| `provider` | `sensevoice`、`zipformer`、`volcano` 等 |
| `startedAtMs` | 当前会议音频流内的起始毫秒偏移 |
| `endedAtMs` | 当前会议音频流内的结束毫秒偏移 |

## Go 侧归一化目标

本地 ASR adapter 后续要把服务返回事件转换成：

```go
stt.TranscriptResult
```

然后继续走现有链路：

```text
TranscriptResult
  ↓
修改模式 / 唤醒词判断
  ↓
普通会议正文入库
  ↓
结构化分析
  ↓
WebSocket 推送前端
```

## 当前完成状态

已完成：

- Go 侧 ASR 协议常量。
- Go 侧统一 `stt.Adapter` 接口。
- C++ `local-asr-service` 最小服务骨架。
- C++ 服务支持 VAD 切段、mock 文本、外部 SenseVoice 命令调用。
- C++ 服务支持可选 sherpa-onnx C API SenseVoice 引擎。
- Go `STT_PROVIDER=local` adapter，可以通过 `LOCAL_ASR_URL` 调用本地 ASR 服务。
- Python `sensevoice_server.py` 支持可选 CAM++ speaker embedding 与在线聚类，输出 `speaker_1 / speaker_2 / ...`。

未完成：

- 在 RK3588 上链接真实 sherpa-onnx/RKNN 库并验证模型文件。
- 在 RK3588 C++ 服务中接入 CAM++ / RKNN speaker embedding。

## CAM++ 发言人分离

Windows/Ubuntu 开发脚本支持两种 CAM++ 接入方式：

```powershell
# FunASR / ModelScope 本地模型路径或模型名
$env:CAMPP_MODEL="iic/speech_campplus_sv_zh-cn_16k-common"

# 或者使用外部命令，{wav} 会替换成当前语音段 wav 文件
$env:CAMPP_COMMAND="python campp_embed.py --wav {wav}"
```

外部命令输出可以是：

```json
{"embedding":[0.1,0.2,0.3]}
```

也可以直接返回：

```json
{"speakerLabel":"speaker_2"}
```

聚类参数：

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

没有配置 CAM++ 模型或命令时，服务会稳定回退到 `speaker_1`，不影响 SenseVoice 转写链路。

这几个属于下一阶段。
