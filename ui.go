package main

import "net/http"

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// indexHTML is intentionally embedded for the local MVP dashboard; it keeps the
// demo self-contained until the desktop shell or a real frontend app lands.
const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI 会议秘书控制台</title>
  <style>
    :root {
      --bg: #f4f6fb;
      --panel: #ffffff;
      --panel-soft: #f8fafc;
      --text: #111827;
      --muted: #64748b;
      --line: #d8dee9;
      --blue: #2563eb;
      --green: #16a34a;
      --amber: #d97706;
      --red: #dc2626;
      --ink: #0f172a;
    }

    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
    }
    main {
      max-width: 1440px;
      margin: 0 auto;
      padding: 24px;
    }
    header {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 20px;
      margin-bottom: 16px;
    }
    h1 {
      margin: 0;
      font-size: 24px;
      letter-spacing: 0;
    }
    h2 {
      margin: 0 0 12px;
      font-size: 15px;
      letter-spacing: 0;
    }
    .subtitle {
      margin: 6px 0 0;
      color: var(--muted);
      font-size: 13px;
    }
    .status-strip {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
      gap: 10px;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      border: 1px solid var(--line);
      background: var(--panel);
      border-radius: 999px;
      padding: 8px 12px;
      font-size: 13px;
      white-space: nowrap;
    }
    .status-dot {
      width: 9px;
      height: 9px;
      border-radius: 50%;
      background: #94a3b8;
    }
    .recording .status-dot { background: var(--green); }
    .soft_paused .status-dot { background: var(--amber); }
    .ended .status-dot { background: #475569; }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
    }
    .control-panel {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 16px;
      align-items: center;
      margin-bottom: 16px;
    }
    .button-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    button {
      border: 1px solid #cbd5e1;
      background: #ffffff;
      color: var(--text);
      border-radius: 6px;
      padding: 9px 12px;
      cursor: pointer;
      font-size: 13px;
    }
    button:hover { background: #f1f5f9; }
    button.primary {
      background: var(--blue);
      border-color: var(--blue);
      color: #ffffff;
    }
    button.danger {
      border-color: #fecaca;
      color: #b91c1c;
    }
    input {
      border: 1px solid #cbd5e1;
      border-radius: 6px;
      padding: 9px 10px;
      font-size: 13px;
      width: 100%;
      min-width: 0;
    }
    .mock-row {
      display: grid;
      grid-template-columns: minmax(260px, 1fr) auto;
      gap: 8px;
      margin-top: 12px;
    }
    .workspace {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      gap: 16px;
      align-items: start;
      margin-top: 16px;
    }
    .transcript-box {
      background: var(--ink);
      color: #e5e7eb;
      border-radius: 8px;
      padding: 18px 20px;
      height: 560px;
      overflow-y: auto;
      line-height: 1.9;
      font-size: 15px;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .transcript-panel {
      margin-top: 16px;
    }
    .transcript-panel .transcript-box {
      height: 300px;
      box-shadow: inset 0 0 0 1px rgba(255,255,255,0.04);
    }
    .placeholder { color: #94a3b8; }
    .intelligence-stack { display: grid; gap: 12px; }
    .section-heading,
    .board-heading {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 12px;
    }
    .section-heading h2,
    .board-heading h2 {
      margin-bottom: 0;
    }
    .section-note {
      margin: 4px 0 0;
      color: var(--muted);
      font-size: 12px;
    }
    .eyebrow {
      display: block;
      margin-bottom: 4px;
      color: var(--muted);
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0;
      text-transform: uppercase;
    }
    .board-panel {
      min-height: 540px;
      overflow: hidden;
    }
    .board-count {
      min-width: 42px;
      height: 42px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel-soft);
      color: var(--text);
      font-size: 22px;
      line-height: 1;
    }
    .action-board { border-top: 3px solid var(--blue); }
    .decision-board { border-top: 3px solid var(--green); }
    .insight-row {
      display: grid;
      grid-template-columns: minmax(0, 0.95fr) minmax(420px, 1.05fr);
      gap: 16px;
      align-items: start;
      margin-top: 16px;
    }
    .summary-actions {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .mini-count,
    .live-chip {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-width: 34px;
      height: 28px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 0 10px;
      background: var(--panel-soft);
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
    }
    .live-chip {
      color: #15803d;
      background: #ecfdf5;
      border-color: #bbf7d0;
    }
    .intelligence-toolbar {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 8px;
      align-items: start;
      margin-bottom: 12px;
    }
    .intelligence-toolbar .metric-row {
      margin-bottom: 0;
    }
    .summary-text {
      color: #1f2937;
      line-height: 1.65;
      max-height: 150px;
      overflow-y: auto;
      font-size: 14px;
    }
    .metric-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 8px;
      margin-bottom: 12px;
    }
    .metric {
      background: var(--panel-soft);
      border: 1px solid #e2e8f0;
      border-radius: 8px;
      padding: 10px;
    }
    .metric strong {
      display: block;
      font-size: 20px;
      line-height: 1;
      margin-bottom: 4px;
    }
    .metric span {
      color: var(--muted);
      font-size: 12px;
    }
    .mic-debug {
      display: grid;
      grid-template-columns: repeat(2, minmax(160px, 1fr));
      gap: 10px;
      margin-top: 12px;
      padding-top: 12px;
      border-top: 1px solid #e2e8f0;
      color: var(--muted);
      font-size: 12px;
    }
    .level-track {
      height: 8px;
      margin-top: 6px;
      overflow: hidden;
      background: #e2e8f0;
      border-radius: 999px;
    }
    .level-fill {
      width: 0%;
      height: 100%;
      background: var(--green);
      transition: width 120ms linear;
    }
    .mic-note {
      grid-column: 1 / -1;
      color: #334155;
    }
    .list {
      margin: 0;
      padding: 0;
      list-style: none;
      display: grid;
      gap: 10px;
      max-height: 210px;
      overflow-y: auto;
      position: relative;
    }
    .board-list {
      max-height: 470px;
      padding: 2px 2px 4px;
    }
    .list-item {
      border: 1px solid #e2e8f0;
      background: var(--panel-soft);
      border-radius: 8px;
      padding: 12px;
      line-height: 1.5;
      position: relative;
      overflow: hidden;
      transform-origin: center;
      transition:
        transform 680ms cubic-bezier(.2,.8,.2,1),
        opacity 520ms ease,
        background-color 700ms ease,
        border-color 700ms ease,
        box-shadow 700ms ease;
      will-change: transform, opacity;
    }
    .list-item.placeholder {
      color: var(--muted);
      border-style: dashed;
      background: #ffffff;
    }
    .list-item.is-entering {
      animation: itemEnter 760ms cubic-bezier(.2,.8,.2,1) both;
    }
    .list-item.is-updated {
      z-index: 2;
      animation: itemUpdate 1500ms ease both;
    }
    .list-item.is-removing {
      z-index: 3;
      animation: itemRemove 950ms ease forwards;
      pointer-events: none;
    }
    .list-item.action-completed {
      background: #f8fafc;
      color: #64748b;
    }
    .list-item.action-completed .item-title {
      text-decoration: line-through;
      text-decoration-thickness: 1px;
    }
    .item-title,
    .item-content {
      color: var(--text);
      font-size: 14px;
      font-weight: 650;
      line-height: 1.55;
    }
    .item-index {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 22px;
      height: 22px;
      margin-right: 6px;
      border-radius: 50%;
      background: #dbeafe;
      color: #1d4ed8;
      font-size: 12px;
      font-weight: 700;
    }
    .item-meta {
      margin-top: 6px;
      color: var(--muted);
      font-size: 12px;
    }
    .item-status {
      display: inline-flex;
      align-items: center;
      height: 22px;
      margin-left: 6px;
      padding: 0 8px;
      border-radius: 999px;
      background: #eff6ff;
      color: #1d4ed8;
      font-size: 11px;
      font-weight: 700;
    }
    .item-status.completed {
      background: #ecfdf5;
      color: #15803d;
    }
    .interaction-card {
      border-left: 3px solid var(--blue);
    }
    .interaction-card.is-active {
      animation: interactionPulse 760ms ease;
    }
    .interaction-card.is-processing {
      border-left-color: var(--amber);
      box-shadow: 0 12px 28px rgba(217, 119, 6, 0.12);
    }
    .interaction-body {
      display: grid;
      gap: 12px;
    }
    .interaction-processing {
      display: none;
      align-items: center;
      gap: 10px;
      padding: 10px 12px;
      border: 1px solid #fde68a;
      border-radius: 8px;
      background: #fffbeb;
      color: #92400e;
      font-size: 13px;
      font-weight: 700;
    }
    .interaction-processing.visible {
      display: flex;
    }
    .interaction-spinner {
      width: 16px;
      height: 16px;
      border: 2px solid rgba(217, 119, 6, 0.22);
      border-top-color: var(--amber);
      border-radius: 50%;
      animation: spin 850ms linear infinite;
      flex: 0 0 auto;
    }
    .interaction-message {
      margin: 0;
      white-space: pre-line;
      line-height: 1.6;
      color: #1f2937;
      font-size: 14px;
    }
    .interaction-message.failed {
      color: #b91c1c;
    }
    .interaction-timeline {
      display: grid;
      gap: 8px;
      padding-top: 12px;
      border-top: 1px solid var(--line);
    }
    .interaction-step {
      display: grid;
      grid-template-columns: 108px 1fr;
      gap: 8px;
      align-items: baseline;
      font-size: 12px;
      color: var(--muted);
    }
    .interaction-step strong {
      color: #111827;
      font-size: 13px;
    }
    .interaction-step small {
      display: block;
      margin-top: 2px;
      color: var(--muted);
      font-size: 11px;
      line-height: 1.3;
    }
    .interaction-step.total strong {
      color: var(--blue);
    }
    .share-panel {
      display: none;
      margin-top: 16px;
    }
    .share-panel.visible {
      display: block;
    }
    .share-layout {
      display: grid;
      grid-template-columns: 180px minmax(0, 1fr);
      gap: 16px;
      align-items: center;
    }
    .share-qr {
      width: 160px;
      height: 160px;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: white;
    }
    .share-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 10px;
    }
    .share-link {
      display: block;
      margin-top: 8px;
      color: var(--muted);
      font-size: 12px;
      word-break: break-all;
    }
    .settings {
      margin: 16px 0;
    }
    .config-grid {
      display: grid;
      grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) minmax(220px, 1fr) auto;
      gap: 8px;
      margin-top: 12px;
      align-items: end;
    }
    .field {
      display: grid;
      gap: 6px;
      color: var(--muted);
      font-size: 12px;
    }
    .field.wide {
      min-width: 220px;
    }
    .segmented {
      display: inline-grid;
      grid-auto-flow: column;
      grid-auto-columns: 1fr;
      border: 1px solid #cbd5e1;
      border-radius: 8px;
      overflow: hidden;
      background: #ffffff;
    }
    .segmented button {
      border: 0;
      border-radius: 0;
      padding: 9px 10px;
      background: transparent;
      color: var(--muted);
      white-space: nowrap;
    }
    .segmented button.active {
      background: var(--text);
      color: #ffffff;
    }
    .debug-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 12px;
      margin-top: 12px;
    }
    pre {
      background: var(--ink);
      color: #e5e7eb;
      border-radius: 8px;
      margin: 0;
      padding: 12px;
      overflow: auto;
      max-height: 260px;
      font-size: 12px;
    }
    .notice {
      color: var(--muted);
      font-size: 12px;
      margin: 8px 0 0;
    }
    @media (max-width: 980px) {
      main { padding: 16px; }
      header, .control-panel { grid-template-columns: 1fr; display: grid; }
      .status-strip { justify-content: flex-start; }
      .workspace, .insight-row { grid-template-columns: 1fr; }
      .transcript-box { height: 420px; }
      .config-grid, .mock-row, .debug-grid, .intelligence-toolbar { grid-template-columns: 1fr; }
    }
    @keyframes itemEnter {
      0% { opacity: 0; transform: translateY(18px) scale(0.96); background: #dcfce7; border-color: #22c55e; box-shadow: 0 0 0 0 rgba(34, 197, 94, 0); }
      45% { opacity: 1; transform: translateY(0) scale(1.018); background: #dcfce7; border-color: #22c55e; box-shadow: 0 16px 34px rgba(34, 197, 94, 0.22), 0 0 0 4px rgba(34, 197, 94, 0.14); }
      100% { opacity: 1; transform: translateY(0) scale(1); box-shadow: none; }
    }
    @keyframes itemUpdate {
      0% { transform: scale(1); background: #fff7ed; border-color: #fb923c; box-shadow: 0 0 0 0 rgba(251, 146, 60, 0); }
      18% { transform: scale(1.026); background: #fef3c7; border-color: #f59e0b; box-shadow: 0 18px 38px rgba(245, 158, 11, 0.22), 0 0 0 5px rgba(245, 158, 11, 0.18); }
      62% { transform: scale(1.012); background: #fffbeb; border-color: #f59e0b; box-shadow: 0 12px 28px rgba(245, 158, 11, 0.15), 0 0 0 3px rgba(245, 158, 11, 0.12); }
      100% { transform: scale(1); background: var(--panel-soft); border-color: #e2e8f0; box-shadow: none; }
    }
    @keyframes itemRemove {
      0% { opacity: 1; transform: translateX(0) scale(1); max-height: 180px; background: #fef2f2; border-color: #ef4444; box-shadow: 0 0 0 0 rgba(239, 68, 68, 0); }
      28% { opacity: 1; transform: translateX(0) scale(1.018); background: #fee2e2; border-color: #dc2626; box-shadow: 0 18px 38px rgba(220, 38, 38, 0.22), 0 0 0 5px rgba(220, 38, 38, 0.14); }
      68% { opacity: 0.42; transform: translateX(34px) scale(0.97); background: #fef2f2; border-color: #fecaca; }
      100% { opacity: 0; transform: translateX(54px) scale(0.94); max-height: 0; padding-top: 0; padding-bottom: 0; margin: 0; border-width: 0; }
    }
    @keyframes interactionPulse {
      0% { box-shadow: 0 0 0 rgba(37, 99, 235, 0); }
      35% { box-shadow: 0 16px 34px rgba(37, 99, 235, 0.14); }
      100% { box-shadow: 0 0 0 rgba(37, 99, 235, 0); }
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
  </style>
</head>
<body>
<main>
  <header>
    <div>
      <h1>AI 会议秘书控制台</h1>
      <p class="subtitle">实时转写、结构化摘要、决策与待办提取</p>
    </div>
    <div class="status-strip">
      <div id="statusBadge" class="pill">
        <span class="status-dot"></span>
        <strong id="statusText">standby</strong>
      </div>
      <div class="pill">会议 <span id="meetingId">none</span></div>
      <div class="pill">时长 <span id="duration">0s</span></div>
      <div class="pill">麦克风 <span id="micState">未连接</span></div>
    </div>
  </header>

  <section class="panel control-panel">
    <div>
      <div class="button-row">
        <button class="primary" onclick="createMeeting()">创建会议</button>
        <button onclick="action('start')">开始会议并开启麦克风</button>
        <button onclick="action('pause')">暂停</button>
        <button onclick="action('resume')">恢复</button>
        <button class="danger" onclick="action('end')">结束</button>
        <button onclick="refreshAll()">刷新</button>
      </div>
      <div class="mock-row">
        <input id="utteranceText" value="张三下周五前完成测试环境联调，李四负责整理上线风险。">
        <button onclick="addUtterance()">添加测试转写</button>
      </div>
      <div class="mic-debug">
        <div>
          本地输入 <strong id="localPeak">0</strong>
          <div class="level-track"><div id="localLevel" class="level-fill"></div></div>
        </div>
        <div>
          后端收到 <strong id="serverPeak">0</strong>
          <div class="level-track"><div id="serverLevel" class="level-fill"></div></div>
        </div>
        <div>采样率 <strong id="sampleRateLabel">-</strong></div>
        <div>音频包 <strong id="packetCountLabel">0</strong></div>
        <div id="micDiagnosis" class="mic-note">等待麦克风连接。</div>
      </div>
    </div>
  </section>

  <section id="sharePanel" class="panel share-panel">
    <div class="share-layout">
      <img id="shareQR" class="share-qr" alt="会议纪要二维码">
      <div>
        <h2>会议纪要二维码</h2>
        <p class="notice">手机扫码查看会议纪要，并下载 Markdown 文档。</p>
        <a id="shareLink" class="share-link" href="#" target="_blank" rel="noreferrer"></a>
        <div class="share-actions">
          <button onclick="openSharePage()">打开纪要页</button>
          <button onclick="downloadMarkdown()">下载 Markdown</button>
        </div>
      </div>
    </div>
  </section>

  <section class="panel settings">
    <div class="section-heading">
      <div>
        <h2>LLM 配置</h2>
        <p class="section-note">结构化和会中交互可以分别使用不同模型。</p>
      </div>
      <p class="notice" id="llmConfigStatus">LLM 配置未加载</p>
    </div>
    <div class="config-grid">
      <label class="field wide">Use case
        <input id="llmUseCase" type="hidden" value="default">
        <div class="segmented" id="llmUseCaseButtons">
          <button type="button" data-value="default" onclick="setLLMUseCase('default')">Default</button>
          <button type="button" data-value="intelligence" onclick="setLLMUseCase('intelligence')">Intelligence</button>
          <button type="button" data-value="interaction" onclick="setLLMUseCase('interaction')">Interaction</button>
        </div>
      </label>
      <label class="field">Base URL
        <input id="llmBaseURL" placeholder="https://api.example.com/v1">
      </label>
      <label class="field">API Key
        <input id="llmAPIKey" type="password" placeholder="留空表示保留已保存 Key">
      </label>
      <label class="field">Model
        <input id="llmModel" placeholder="model-name">
      </label>
      <label class="field wide">Thinking
        <input id="llmThinkingMode" type="hidden" value="default">
        <div class="segmented" id="llmThinkingButtons">
          <button type="button" data-value="default" onclick="setThinkingMode('default')">Default</button>
          <button type="button" data-value="disabled" onclick="setThinkingMode('disabled')">Disabled</button>
          <button type="button" data-value="enabled" onclick="setThinkingMode('enabled')">Enabled</button>
        </div>
      </label>
      <button onclick="saveLLMConfig()">保存配置</button>
    </div>
  </section>

  <div class="workspace">
    <section class="panel board-panel action-board">
      <div class="board-heading">
        <div>
          <span class="eyebrow">Action Items</span>
          <h2>待办事项</h2>
        </div>
        <strong id="actionCount" class="board-count">0</strong>
      </div>
      <ol id="actionItemsList" class="list board-list"></ol>
    </section>

    <section class="panel board-panel decision-board">
      <div class="board-heading">
        <div>
          <span class="eyebrow">Decisions</span>
          <h2>会议决策</h2>
        </div>
        <strong id="decisionCount" class="board-count">0</strong>
      </div>
      <ol id="decisionsList" class="list board-list"></ol>
    </section>
  </div>

  <section class="insight-row">
    <div class="panel summary-panel">
      <div class="section-heading">
        <div>
          <span class="eyebrow">Summary</span>
          <h2>会议摘要</h2>
        </div>
        <div class="summary-actions">
          <span id="summaryCount" class="mini-count">0</span>
          <button onclick="analyzeNow()">立即结构化</button>
        </div>
      </div>
      <div id="summaryBox" class="summary-text placeholder">暂无摘要</div>
    </div>

    <div class="panel interaction-card">
      <div class="section-heading">
        <div>
          <span class="eyebrow">Interaction</span>
          <h2>语音交互</h2>
        </div>
        <span class="live-chip">Live</span>
      </div>
      <div class="interaction-body">
        <div id="interactionProcessing" class="interaction-processing">
          <span class="interaction-spinner"></span>
          <span id="interactionProcessingText">正在处理语音指令...</span>
        </div>
        <p id="interactionMessage" class="interaction-message placeholder">等待语音指令...</p>
        <div id="interactionTimeline" class="interaction-timeline" style="display:none;"></div>
      </div>
    </div>
  </section>

  <section class="panel transcript-panel">
    <div class="section-heading">
      <div>
        <span class="eyebrow">Transcript</span>
        <h2>全文转写</h2>
      </div>
      <span class="mini-count">Live</span>
    </div>
    <div id="fullTranscript" class="transcript-box">
      <span class="placeholder">等待转写内容...</span>
    </div>
  </section>

  <div class="debug-sinks" hidden>
    <pre id="events">[]</pre>
    <pre id="utterances">[]</pre>
    <pre id="intelligenceRaw">null</pre>
    <pre id="interactionsRaw">[]</pre>
  </div>
</main>

<script>
let currentMeetingId = "";
let audioWS = null;
let audioContext = null;
let micStream = null;
let browserMediaStream = null;
let processor = null;
let browserAudioActive = false;
let currentStatus = "standby";
let audioWSMeetingId = "";
let currentSampleRate = 0;
let activeInteractionEventId = "";
const animatedListState = {
  action_items: { initialized: false, signatures: new Map() },
  decisions: { initialized: false, signatures: new Map() }
};

function renderState(update) {
  if (!update) return;
  const status = update.status || "standby";
  currentStatus = status;
  const badge = document.getElementById("statusBadge");
  badge.className = "pill " + status;
  document.getElementById("statusText").textContent = status;
  currentMeetingId = update.meetingId || (update.meeting && update.meeting.id) || currentMeetingId;
  document.getElementById("meetingId").textContent = currentMeetingId || "none";
  document.getElementById("duration").textContent = (update.durationSeconds || 0) + "s";
  renderSharePanel(status);
  if (currentMeetingId) refreshLists();
}

function renderSharePanel(status) {
  const panel = document.getElementById("sharePanel");
  const qr = document.getElementById("shareQR");
  const link = document.getElementById("shareLink");
  if (!panel || !qr || !link) return;
  if (status !== "ended" || !currentMeetingId) {
    panel.className = "panel share-panel";
    qr.removeAttribute("src");
    link.href = "#";
    link.textContent = "";
    return;
  }
  const sharePath = "/share/" + encodeURIComponent(currentMeetingId);
  const qrPath = "/api/meetings/" + encodeURIComponent(currentMeetingId) + "/share-qr.png?t=" + Date.now();
  panel.className = "panel share-panel visible";
  qr.src = qrPath;
  link.href = sharePath;
  link.textContent = location.origin + sharePath;
}

function openSharePage() {
  if (!currentMeetingId) return;
  window.open("/share/" + encodeURIComponent(currentMeetingId), "_blank", "noopener,noreferrer");
}

function downloadMarkdown() {
  if (!currentMeetingId) return;
  window.open("/api/meetings/" + encodeURIComponent(currentMeetingId) + "/minutes.md", "_blank", "noopener,noreferrer");
}

async function createMeeting() {
  stopBrowserAudio();
  const res = await fetch("/api/meetings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title: "调试会议", type: "general", operator: "screen" })
  });
  await handleResponse(res);
}

async function action(name) {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  if (name === "start" && currentStatus === "recording") {
    try {
      await startBrowserAudio();
    } catch (err) {
      console.error(err);
      alert("会议正在记录中，但麦克风没有开启。请检查浏览器权限后重试。");
    }
    return;
  }
  const res = await fetch("/api/meetings/" + currentMeetingId + "/" + name, { method: "POST" });
  const ok = res.ok;
  const body = await handleResponse(res);
  if (!ok) return;

  const nextStatus = (body && body.status) || currentStatus;
  if (name === "end") {
    stopBrowserAudio();
    return;
  }
  if (name === "pause") {
    return;
  }

  if (name === "start" || name === "resume" || nextStatus === "recording") {
    try {
      if (browserAudioActive && audioWS && audioWS.readyState === WebSocket.OPEN && audioWSMeetingId === currentMeetingId) {
        return;
      }
      stopBrowserAudio();
      await startBrowserAudio();
    } catch (err) {
      console.error(err);
      alert("会议已进入记录中，但麦克风没有开启。请检查浏览器权限后重新点击开始或恢复。");
    }
  }
}

async function addUtterance() {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  const text = document.getElementById("utteranceText").value;
  const res = await fetch("/api/meetings/" + currentMeetingId + "/utterances", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ speakerLabel: "speaker_1", text })
  });
  await handleResponse(res);
}

async function startBrowserAudio() {
  if (!currentMeetingId) {
    alert("请先创建会议并开始会议");
    return;
  }
  if (browserAudioActive && audioWS && audioWS.readyState === WebSocket.OPEN && audioWSMeetingId === currentMeetingId) {
    return;
  }
  if (browserAudioActive || audioWS) {
    stopBrowserAudio();
  }

  try {
    const wsMeetingId = currentMeetingId;
    audioWSMeetingId = wsMeetingId;
    audioWS = new WebSocket("ws://" + location.host + "/api/meetings/" + wsMeetingId + "/audio/ws");
    audioWS.binaryType = "arraybuffer";
    audioWS.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "audio_blocked") console.warn("audio blocked", msg.status);
        if (msg.type === "error") console.error(msg.error);
        if (msg.type === "audio_metrics") {
          updateServerMicMetrics(msg.peak || 0, msg.packets || 0);
        }
      } catch(e) {}
    };
    audioWS.onclose = () => {
      if (audioWSMeetingId === wsMeetingId) {
        cleanupBrowserAudio(false);
      }
    };
    audioWS.onerror = () => {
      if (audioWSMeetingId === wsMeetingId) {
        document.getElementById("micState").textContent = "连接异常";
      }
    };
    await new Promise((resolve, reject) => {
      audioWS.onopen = resolve;
      audioWS.onerror = reject;
    });
    audioWS.onerror = () => {
      if (audioWSMeetingId === wsMeetingId) {
        document.getElementById("micState").textContent = "连接异常";
      }
    };

    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        sampleRate: 16000,
        echoCancellation: false,
        noiseSuppression: false,
        autoGainControl: false
      }
    });
    browserMediaStream = stream;
    audioContext = new AudioContext({ sampleRate: 16000 });
    currentSampleRate = audioContext.sampleRate || 16000;
    micStream = audioContext.createMediaStreamSource(stream);
    processor = audioContext.createScriptProcessor(1024, 1, 1);

    processor.onaudioprocess = (e) => {
      if (!browserAudioActive || !audioWS || audioWS.readyState !== WebSocket.OPEN) return;
      const input = e.inputBuffer.getChannelData(0);
      const localPeak = floatPeakToPCM(input);
      const outputSampleCount = targetSampleCount(input.length, currentSampleRate, 16000);
      updateLocalMicMetrics(localPeak);
      if (currentStatus === "soft_paused") {
        audioWS.send(new ArrayBuffer(outputSampleCount * 2));
        return;
      }
      audioWS.send(resampleToPCM16(input, currentSampleRate, 16000));
    };

    browserAudioActive = true;
    document.getElementById("micState").textContent = "已连接";
    document.getElementById("sampleRateLabel").textContent = Math.round(currentSampleRate) + "Hz";
    micStream.connect(processor);
    processor.connect(audioContext.destination);
    stream.getTracks().forEach(t => t.enabled = true);
  } catch (err) {
    stopBrowserAudio();
    throw err;
  }
}

function stopBrowserAudio() {
  cleanupBrowserAudio(true);
}

function cleanupBrowserAudio(closeSocket) {
  browserAudioActive = false;
  document.getElementById("micState").textContent = "未连接";
  const ws = audioWS;
  audioWS = null;
  audioWSMeetingId = "";
  if (closeSocket && ws) {
    try { ws.onclose = null; } catch(e) {}
    try { ws.onerror = null; } catch(e) {}
    if (ws.readyState === WebSocket.OPEN) {
      try { ws.send(JSON.stringify({ type: "end" })); } catch(e) {}
    }
    if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
      try { ws.close(); } catch(e) {}
    }
  }
  if (processor) { try { processor.disconnect(); } catch(e) {} processor = null; }
  if (micStream) { try { micStream.disconnect(); } catch(e) {} micStream = null; }
  if (browserMediaStream) {
    try { browserMediaStream.getTracks().forEach(track => track.stop()); } catch(e) {}
    browserMediaStream = null;
  }
  if (audioContext) { try { audioContext.close(); } catch(e) {} audioContext = null; }
  updateLocalMicMetrics(0);
  updateServerMicMetrics(0, 0);
}

function targetSampleCount(inputLength, sourceRate, targetRate) {
  return Math.max(1, Math.round(inputLength * targetRate / sourceRate));
}

function resampleToPCM16(input, sourceRate, targetRate) {
  const outputLength = targetSampleCount(input.length, sourceRate, targetRate);
  const buf = new ArrayBuffer(outputLength * 2);
  const view = new DataView(buf);
  const ratio = sourceRate / targetRate;

  for (let i = 0; i < outputLength; i++) {
    const sourceIndex = i * ratio;
    const left = Math.floor(sourceIndex);
    const right = Math.min(left + 1, input.length - 1);
    const weight = sourceIndex - left;
    const sample = input[left] * (1 - weight) + input[right] * weight;
    const s = Math.max(-1, Math.min(1, sample));
    view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return buf;
}

function floatPeakToPCM(input) {
  let peak = 0;
  for (let i = 0; i < input.length; i++) {
    const value = Math.abs(input[i]);
    if (value > peak) peak = value;
  }
  return Math.round(Math.min(1, peak) * 32767);
}

function updateLocalMicMetrics(peak) {
  const el = document.getElementById("localPeak");
  if (!el) return;
  el.textContent = String(peak);
  setLevel("localLevel", peak);
  updateMicDiagnosis();
}

function updateServerMicMetrics(peak, packets) {
  const el = document.getElementById("serverPeak");
  if (!el) return;
  el.textContent = String(peak);
  document.getElementById("packetCountLabel").textContent = String(packets || 0);
  setLevel("serverLevel", peak);
  updateMicDiagnosis();
}

function setLevel(id, peak) {
  const level = document.getElementById(id);
  if (!level) return;
  const percent = Math.max(0, Math.min(100, Math.round((peak / 3000) * 100)));
  level.style.width = percent + "%";
}

function updateMicDiagnosis() {
  const box = document.getElementById("micDiagnosis");
  if (!box) return;
  const localPeak = Number(document.getElementById("localPeak").textContent || 0);
  const serverPeak = Number(document.getElementById("serverPeak").textContent || 0);
  if (!browserAudioActive) {
    box.textContent = "等待麦克风连接。";
  } else if (localPeak < 100) {
    box.textContent = "本地输入很低：优先检查系统麦克风、浏览器权限、输入设备或麦克风音量。";
  } else if (serverPeak < 100) {
    box.textContent = "本地有声音，但后端收到很低：网页编码或发送链路可能有问题。";
  } else {
    box.textContent = "麦克风和网页发送链路都有声音，若仍不卡字，问题更可能在 ASR 返回。";
  }
}

async function handleResponse(res) {
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    alert(body.error || "请求失败");
    return body;
  }
  if (body.type === "state_update") renderState(body);
  if (body.type === "transcript_delta") refreshLists();
  if (body.type === "intelligence_update") renderIntelligence(body);
  if (body.state) renderState(body.state);
  return body;
}

async function refreshAll() {
  const res = await fetch("/api/state");
  const body = await res.json();
  renderState(body);
  await loadLLMConfig();
}

async function refreshLists() {
  if (!currentMeetingId) return;
  const [eventsRes, utterRes, intelRes, interactionsRes] = await Promise.all([
    fetch("/api/meetings/" + currentMeetingId + "/events"),
    fetch("/api/meetings/" + currentMeetingId + "/utterances"),
    fetch("/api/meetings/" + currentMeetingId + "/intelligence"),
    fetch("/api/meetings/" + currentMeetingId + "/interactions")
  ]);
  const events = await eventsRes.json().catch(() => []);
  const utterances = await utterRes.json().catch(() => []);
  const intelligence = await intelRes.json().catch(() => null);
  const interactions = await interactionsRes.json().catch(() => []);
  document.getElementById("events").textContent = JSON.stringify(events, null, 2);
  document.getElementById("utterances").textContent = JSON.stringify(utterances, null, 2);
  const interactionsRaw = document.getElementById("interactionsRaw");
  if (interactionsRaw) interactionsRaw.textContent = JSON.stringify(interactions, null, 2);
  renderFullTranscript(utterances);
  renderIntelligence(intelligence);
  renderInteractionHistory(interactions);
}

function setLLMUseCase(value) {
  const input = document.getElementById("llmUseCase");
  if (!input) return;
  input.value = value;
  syncSegmentedButtons("llmUseCaseButtons", value);
  loadLLMConfig();
}

function setThinkingMode(value) {
  const input = document.getElementById("llmThinkingMode");
  if (!input) return;
  input.value = value;
  syncSegmentedButtons("llmThinkingButtons", value);
}

function syncSegmentedButtons(groupId, value) {
  const group = document.getElementById(groupId);
  if (!group) return;
  group.querySelectorAll("button").forEach(button => {
    button.classList.toggle("active", button.dataset.value === value);
  });
}

async function loadLLMConfig() {
  const useCaseEl = document.getElementById("llmUseCase");
  const useCase = useCaseEl ? useCaseEl.value : "default";
  const res = await fetch("/api/llm-config?useCase=" + encodeURIComponent(useCase));
  const cfg = await res.json().catch(() => null);
  if (!cfg) return;
  if (useCaseEl) useCaseEl.value = cfg.useCase || useCase;
  syncSegmentedButtons("llmUseCaseButtons", cfg.useCase || useCase);
  document.getElementById("llmBaseURL").value = cfg.baseUrl || "";
  document.getElementById("llmModel").value = cfg.model || "";
  document.getElementById("llmThinkingMode").value = cfg.thinkingMode || "default";
  syncSegmentedButtons("llmThinkingButtons", cfg.thinkingMode || "default");
  document.getElementById("llmAPIKey").value = "";
  /*
  document.getElementById("llmConfigStatus").textContent =
    "当前模型：" + (cfg.model || "未配置") + "，Key：" + (cfg.hasApiKey ? "已保存" : "未保存");
  */
  document.getElementById("llmConfigStatus").textContent =
    "Use case: " + (cfg.useCase || useCase) +
    " | Model: " + (cfg.model || "not configured") +
    " | Key: " + (cfg.hasApiKey ? "saved" : "not saved") +
    " | Thinking: " + (cfg.thinkingMode || "default");
}

async function saveLLMConfig() {
  const res = await fetch("/api/llm-config", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      useCase: document.getElementById("llmUseCase").value,
      baseUrl: document.getElementById("llmBaseURL").value,
      apiKey: document.getElementById("llmAPIKey").value,
      model: document.getElementById("llmModel").value,
      thinkingMode: document.getElementById("llmThinkingMode").value
    })
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    alert(body.error || "保存 LLM 配置失败");
    return;
  }
  await loadLLMConfig();
}

async function analyzeNow() {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  renderIntelligenceStatus("正在请求结构化分析...");
  const res = await fetch("/api/meetings/" + currentMeetingId + "/intelligence/analyze", { method: "POST" });
  await handleResponse(res);
}

function renderIntelligence(snapshot) {
  const summaryBox = document.getElementById("summaryBox");
  const decisionsList = document.getElementById("decisionsList");
  const actionItemsList = document.getElementById("actionItemsList");
  const raw = document.getElementById("intelligenceRaw");
  if (!summaryBox || !decisionsList || !actionItemsList || !raw) return;

  const decisions = snapshot && Array.isArray(snapshot.decisions) ? snapshot.decisions : [];
  const actions = snapshot && Array.isArray(snapshot.actionItems) ? snapshot.actionItems : [];
  const summary = snapshot && snapshot.summary ? snapshot.summary.content : "";

  document.getElementById("summaryCount").textContent = snapshot && snapshot.summary ? "1" : "0";
  document.getElementById("decisionCount").textContent = decisions.length;
  document.getElementById("actionCount").textContent = actions.length;

  summaryBox.textContent = summary || "暂无摘要";
  summaryBox.className = summary ? "summary-text" : "summary-text placeholder";

  renderAnimatedList(decisionsList, decisions, {
    kind: "decisions",
    emptyText: "暂无会议决策",
    key: item => item.id || item.content,
    className: () => "",
    html: (item, index) => {
      return '<span class="item-index">' + (index + 1) + '</span>' +
        '<span class="item-content">' + escapeHtml(item.content || "") + '</span>';
    }
  });
  renderAnimatedList(actionItemsList, actions, {
    kind: "action_items",
    emptyText: "暂无待办事项",
    key: item => item.id || item.task,
    className: item => normalizeItemStatus(item.status) === "completed" ? "action-completed" : "",
    html: (item, index) => {
      const owner = item.owner || "负责人待定";
      const due = item.dueDate || "时间待定";
      const status = normalizeItemStatus(item.status || "open");
      return '<span class="item-index">' + (index + 1) + '</span>' +
        '<span class="item-title">' + escapeHtml(item.task || "") + '</span>' +
        '<span class="item-status ' + escapeHtml(status) + '">' + escapeHtml(status) + '</span>' +
        '<div class="item-meta">' + escapeHtml(owner + " / " + due) + '</div>';
    }
  });
  raw.textContent = JSON.stringify(snapshot || null, null, 2);
}

function renderIntelligenceStatus(message) {
  const summaryBox = document.getElementById("summaryBox");
  if (!summaryBox) return;
  if (!summaryBox.textContent || summaryBox.className.includes("placeholder") || message.includes("正在")) {
    summaryBox.textContent = message;
    summaryBox.className = "summary-text placeholder";
  }
}

function renderInteractionHistory(events) {
  if (activeInteractionEventId) return;
  if (!Array.isArray(events) || !events.length) {
    renderInteractionMessage(null, false);
    return;
  }
  renderInteractionMessage(events[events.length - 1], false);
}

function renderInteractionUpdate(update) {
  if (update && update.event) renderInteractionMessage(update.event);
  if (update && update.snapshot) renderIntelligence(update.snapshot);
}

function renderInteractionStatus(update) {
  if (!update) return;
  if (currentMeetingId && update.meetingId && update.meetingId !== currentMeetingId) return;
  activeInteractionEventId = update.eventId || activeInteractionEventId;
  const box = document.getElementById("interactionProcessing");
  const text = document.getElementById("interactionProcessingText");
  const message = document.getElementById("interactionMessage");
  const card = box ? box.closest(".interaction-card") : null;
  if (text) text.textContent = update.message || "正在处理语音指令...";
  if (box) box.classList.add("visible");
  if (card) card.classList.add("is-processing");
  if (message) {
    message.textContent = update.status === "executing" ? "指令已识别，正在执行操作..." : "正在理解语音指令，请稍候...";
    message.className = "interaction-message placeholder";
  }
}

function renderInteractionMessage(event, animate = true) {
  const message = document.getElementById("interactionMessage");
  const timeline = document.getElementById("interactionTimeline");
  const processing = document.getElementById("interactionProcessing");
  if (!message) return;
  if (timeline && timeline.previousElementSibling !== message) {
    message.insertAdjacentElement("afterend", timeline);
  }
  if (processing) {
    processing.classList.remove("visible");
    const card = processing.closest(".interaction-card");
    if (card) card.classList.remove("is-processing");
  }
  if (event && event.id && activeInteractionEventId === event.id) {
    activeInteractionEventId = "";
  }
  if (!event && timeline) {
    timeline.style.display = "none";
    timeline.innerHTML = "";
  }
  if (!event) {
    message.textContent = "等待语音指令...";
    message.className = "interaction-message placeholder";
    return;
  }
  const ok = event.executionStatus === "success";
  message.textContent = event.message || event.errorMessage || "已收到语音指令。";
  message.className = ok ? "interaction-message" : "interaction-message failed";
  const card = message.closest(".interaction-card");
  if (animate && card) {
    restartAnimation(card, "is-active");
  }
  renderInteractionTimeline(timeline, event);
}

function renderInteractionTimeline(timeline, event) {
  if (!timeline) return;
  const received = formatInteractionTime(event.createdAt);
  const completed = formatInteractionTime(event.completedAt || event.createdAt);
  const duration = formatDuration(event.durationMs || 0);
  const stages = event.timing && Array.isArray(event.timing.stages) ? event.timing.stages : [];
  timeline.style.display = "grid";
  if (stages.length) {
    const rows = [
      '<div class="interaction-step"><span>收到指令</span><strong>' + escapeHtml(received) + '</strong></div>'
    ];
    stages.forEach(stage => {
      const detail = stage.detail ? '<small>' + escapeHtml(stage.detail) + '</small>' : '';
      rows.push(
        '<div class="interaction-step"><span>' + escapeHtml(stage.label || stage.key || "阶段") +
        '</span><strong>' + escapeHtml(formatDuration(stage.durationMs || 0)) + detail + '</strong></div>'
      );
    });
    rows.push('<div class="interaction-step"><span>完成指令</span><strong>' + escapeHtml(completed) + '</strong></div>');
    rows.push('<div class="interaction-step total"><span>总耗时</span><strong>' + escapeHtml(duration) + '</strong></div>');
    timeline.innerHTML = rows.join("");
    return;
  }
  timeline.innerHTML =
    '<div class="interaction-step"><span>收到指令</span><strong>' + escapeHtml(received) + '</strong></div>' +
    '<div class="interaction-step"><span>完成指令</span><strong>' + escapeHtml(completed) + '</strong></div>' +
    '<div class="interaction-step total"><span>总耗时</span><strong>' + escapeHtml(duration) + '</strong></div>';
}

function formatInteractionTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" }) +
    "." + String(date.getMilliseconds()).padStart(3, "0");
}

function formatDuration(ms) {
  ms = Number(ms || 0);
  if (ms < 1000) return Math.max(0, Math.round(ms)) + " ms";
  return (ms / 1000).toFixed(2) + " s";
}

function renderAnimatedList(el, items, options) {
  if (!el) return;
  items = Array.isArray(items) ? items : [];
  const state = animatedListState[options.kind] || { initialized: false, signatures: new Map() };
  animatedListState[options.kind] = state;

  const oldRects = new Map();
  el.querySelectorAll(".list-item[data-key]:not(.placeholder)").forEach(node => {
    oldRects.set(node.dataset.key, node.getBoundingClientRect());
  });

  const nextKeys = new Set(items.map((item, index) => stableListKey(options.key(item, index), index, options.kind)));
  el.querySelectorAll(".list-item.placeholder").forEach(node => node.remove());
  el.querySelectorAll(".list-item[data-key]:not(.placeholder)").forEach(node => {
    if (!nextKeys.has(node.dataset.key) && !node.classList.contains("is-removing")) {
      node.classList.remove("is-entering", "is-updated");
      node.classList.add("is-removing");
      window.setTimeout(() => {
        node.remove();
        ensureListPlaceholder(el, items, options.emptyText);
      }, 980);
    }
  });

  items.forEach((item, index) => {
    const key = stableListKey(options.key(item, index), index, options.kind);
    const signature = options.html(item, index);
    let node = el.querySelector('.list-item[data-key="' + cssEscape(key) + '"]');
    const isNew = !node;
    if (!node) {
      node = document.createElement("li");
      node.dataset.key = key;
      node.className = "list-item is-entering";
    }

    const extraClass = options.className ? options.className(item, index) : "";
    node.className = ("list-item " + extraClass + (isNew ? " is-entering" : "")).trim();
    if (node.innerHTML !== signature) {
      node.innerHTML = signature;
      if (!isNew && state.signatures.get(key) && state.signatures.get(key) !== signature) {
        restartAnimation(node, "is-updated");
      }
    }

    const reference = visibleListNodeAt(el, index);
    if (reference !== node) {
      el.insertBefore(node, reference || null);
    }
  });

  items.forEach((item, index) => {
    const key = stableListKey(options.key(item, index), index, options.kind);
    const node = el.querySelector('.list-item[data-key="' + cssEscape(key) + '"]');
    const oldRect = oldRects.get(key);
    if (!node || !oldRect || node.classList.contains("is-entering")) return;
    const newRect = node.getBoundingClientRect();
    const dx = oldRect.left - newRect.left;
    const dy = oldRect.top - newRect.top;
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return;
    node.style.transition = "none";
    node.style.transform = "translate(" + dx + "px, " + dy + "px)";
    node.offsetHeight;
    node.style.transition = "";
    node.style.transform = "";
  });

  state.signatures = new Map(items.map((item, index) => {
    const key = stableListKey(options.key(item, index), index, options.kind);
    return [key, options.html(item, index)];
  }));
  state.initialized = true;
  ensureListPlaceholder(el, items, options.emptyText);
}

function visibleListNodeAt(el, index) {
  const nodes = Array.from(el.children).filter(node =>
    node.classList.contains("list-item") && !node.classList.contains("is-removing") && !node.classList.contains("placeholder")
  );
  return nodes[index] || null;
}

function ensureListPlaceholder(el, items, text) {
  const hasLiveItems = Array.from(el.children).some(node =>
    node.classList.contains("list-item") && !node.classList.contains("is-removing") && !node.classList.contains("placeholder")
  );
  if (items.length || hasLiveItems) {
    el.querySelectorAll(".list-item.placeholder").forEach(node => node.remove());
    return;
  }
  if (!el.querySelector(".list-item.placeholder")) {
    const li = document.createElement("li");
    li.className = "list-item placeholder";
    li.textContent = text || "暂无内容";
    el.appendChild(li);
  }
}

function stableListKey(value, index, kind) {
  value = value == null ? "" : String(value).trim();
  return value || kind + "_" + index;
}

function normalizeItemStatus(value) {
  value = String(value || "open").trim().toLowerCase();
  if (value === "done" || value === "complete") return "completed";
  return value || "open";
}

function cssEscape(value) {
  if (window.CSS && CSS.escape) return CSS.escape(value);
  return String(value).replace(/["\\]/g, "\\$&");
}

function restartAnimation(node, className) {
  node.classList.remove(className);
  node.offsetHeight;
  node.classList.add(className);
}

function renderFullTranscript(utterances) {
  const transcript = document.getElementById("fullTranscript");
  if (!transcript) return;

  const items = Array.isArray(utterances) ? utterances.filter(item => !item.isDeleted) : [];
  if (!items.length) {
    transcript.innerHTML = '<span class="placeholder">等待转写内容...</span>';
    return;
  }

  const paragraphs = rebuildArticleTranscript(items);
  const html = paragraphs.map(text => '<span>' + escapeHtml(text) + '</span>').join('');
  transcript.innerHTML = html || '<span class="placeholder">等待转写内容...</span>';
  transcript.scrollTop = transcript.scrollHeight;
}

function rebuildArticleTranscript(utterances) {
  const completed = [];
  let currentText = "";
  let currentItem = null;
  let lastLen = 0;

  for (const item of utterances) {
    const text = normalizeTranscriptText(item.text || "");
    if (!text) continue;

    const textLen = text.length;
    const shouldCommitPrevious = shouldCommitTranscriptSegment(currentText, text, currentItem, item, lastLen);

    if (shouldCommitPrevious && currentText) {
      pushArticleSegment(completed, currentText);
      currentText = text;
    } else if (!currentText || text.length >= currentText.length) {
      currentText = text;
    }

    currentItem = item;
    lastLen = textLen;
  }

  if (currentText) {
    pushArticleSegment(completed, currentText);
  }
  return completed;
}

function shouldCommitTranscriptSegment(prev, next, prevItem, nextItem, lastLen) {
  if (!prev || !next) return false;
  if (next.startsWith(prev) || prev.startsWith(next)) return false;

  const textLen = next.length;
  const gapMs = utteranceGapMs(prevItem, nextItem);
  if (gapMs >= 700 && isIndependentAfterPause(prev, next)) return true;
  if (textLen < 5 && lastLen >= 8 && textLen < lastLen * 0.6) return true;
  if (lastLen > 0 && textLen < lastLen * 0.6) return true;
  return isLikelyNewIndependentSegment(prev, next);
}

function pushArticleSegment(segments, text) {
  const normalized = normalizeTranscriptText(text);
  if (!normalized) return;

  const last = segments[segments.length - 1] || "";
  if (last === normalized) return;
  if (last && normalized.startsWith(last)) {
    segments[segments.length - 1] = normalized;
    return;
  }
  if (last && last.startsWith(normalized)) return;

  segments.push(needsLeadingSpace(last, normalized) ? " " + normalized : normalized);
}

function isLikelyNewIndependentSegment(prev, next) {
  if (!prev || !next) return false;
  if (next.startsWith(prev) || prev.startsWith(next)) return false;
  const prefixLen = commonPrefixLength(prev, next);
  const similarity = prefixLen / Math.min(prev.length, next.length);
  const minLen = Math.min(prev.length, next.length);
  if (minLen < 4) return false;
  return similarity < 0.35;
}

function isIndependentAfterPause(prev, next) {
  if (!prev || !next) return false;
  if (next.startsWith(prev) || prev.startsWith(next)) return false;
  if (/[。！？.!?]$/.test(prev)) return true;
  const prefixLen = commonPrefixLength(prev, next);
  const similarity = prefixLen / Math.min(prev.length, next.length);
  return similarity < 0.5;
}

function utteranceGapMs(prevItem, nextItem) {
  if (!prevItem || !nextItem || !prevItem.createdAt || !nextItem.createdAt) return 0;
  const prevTime = Date.parse(prevItem.createdAt);
  const nextTime = Date.parse(nextItem.createdAt);
  if (!Number.isFinite(prevTime) || !Number.isFinite(nextTime)) return 0;
  return Math.max(0, nextTime - prevTime);
}

function commonPrefixLength(a, b) {
  const limit = Math.min(a.length, b.length);
  let i = 0;
  while (i < limit && a[i] === b[i]) i++;
  return i;
}

function normalizeTranscriptText(text) {
  return String(text).replace(/\s+/g, " ").trim();
}

function needsLeadingSpace(prev, next) {
  if (!prev || !next) return false;
  if (/^[，。！？、；,.!?;:]/.test(next)) return false;
  if (/[（(]$/.test(prev)) return false;
  if (isCJK(lastChar(prev)) && isCJK(next[0])) return false;
  return !/[，。！？、；,.!?;:\s]$/.test(prev);
}

function lastChar(value) {
  return value ? value[value.length - 1] : "";
}

function isCJK(ch) {
  if (!ch) return false;
  const code = ch.charCodeAt(0);
  return (code >= 0x3400 && code <= 0x9fff) || (code >= 0xf900 && code <= 0xfaff);
}

function escapeHtml(value) {
  const div = document.createElement("div");
  div.textContent = value == null ? "" : String(value);
  return div.innerHTML;
}

function connectWS() {
  const ws = new WebSocket("ws://" + location.host + "/ws");
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === "state_update") renderState(msg);
    if (msg.type === "transcript_delta") refreshLists();
    if (msg.type === "intelligence_update") renderIntelligence(msg);
    if (msg.type === "interaction_status") renderInteractionStatus(msg);
    if (msg.type === "interaction_update") renderInteractionUpdate(msg);
    if (msg.type === "intelligence_status") {
      renderIntelligenceStatus(msg.message || msg.status);
      console.info(msg.message || msg.status);
    }
    if (msg.type === "intelligence_error") console.error(msg.message);
    if (msg.type === "stt_error") console.error(msg.message);
  };
  ws.onclose = () => setTimeout(connectWS, 1000);
}

refreshAll();
connectWS();
</script>
</body>
</html>`
