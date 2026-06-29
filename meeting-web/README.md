# AI 会议秘书展示前端

这个目录是独立 React 前端，只负责会议大屏展示、语音交互反馈、会议总结翻页展示和二维码弹窗。

Go 后端仍在仓库根目录，负责会议状态机、ASR、结构化提取、会中交互、SQLite、API 与 WebSocket。

## 开发运行

先启动 Go 后端：

```powershell
cd ..
go run .
```

再启动 React 前端开发服务：

```powershell
cd meeting-web
npm install
npm run dev
```

访问：

```text
http://localhost:5173/meeting
```

正式或演示运行时，也可以先构建 React，再由 Go 后端托管：

```powershell
npm run build
cd ..
go run .
```

访问：

```text
http://localhost:8090/meeting
```
