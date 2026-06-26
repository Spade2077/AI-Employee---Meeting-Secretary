# Meeting Core Service

桌面 AI 会议秘书的本地会议核心服务 MVP。

## 功能

- Go HTTP 服务
- 会议状态机：`standby / recording / soft_paused / ending / ended`
- SQLite 本地存储
- WebSocket 推送 `state_update`
- 极简调试页面
- `.env` 配置读取
- 状态变化日志

## 运行

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
.\meeting-core-service.exe
```

默认访问：

```text
http://localhost:8090/debug
```

页面路由：

- `/meeting`：会议大屏展示页
- `/meeting/{meetingId}`：指定会议展示页
- `/admin`：后台设置页
- `/debug`：调试/测试页
- `/share/{meetingId}`：会后纪要分享页

## 配置

可在 `.env` 中配置：

```env
MEETING_CORE_ADDR=:8090
MEETING_CORE_DB=./data/meeting-core.db
STT_PROVIDER=volcano
VOLCANO_APP_ID=your_app_id
VOLCANO_ACCESS_TOKEN=your_access_token
VOLCANO_SECRET_KEY=your_secret_key
```

服务启动时会先读取当前目录 `.env`，再尝试读取 `../volcano-asr-demo/.env`。因此可以直接复用原 demo 的火山凭证文件。

## STT Adapter

当前内置：

- `STT_PROVIDER=volcano`
- `GET /api/meetings/{id}/audio/ws`
- `POST /api/meetings/{id}/utterances`，仅用于手动录入测试转写

后续接本地 ASR 时，实现同一个 adapter 接口或本地 ASR 服务适配即可。会议状态机、大屏事件和数据库不需要跟着供应商改。

标准转写事件：

```json
{
  "type": "transcript_delta",
  "meetingId": "m_xxx",
  "utterance": {
    "id": "u_xxx",
    "speakerLabel": "speaker_1",
    "text": "张三下周五前完成测试环境联调",
    "isFinal": true,
    "provider": "volcano",
    "confidence": 1
  }
}
```
