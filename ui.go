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
      grid-template-columns: minmax(0, 1.25fr) minmax(360px, 0.75fr);
      gap: 16px;
      align-items: start;
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
    .placeholder { color: #94a3b8; }
    .intelligence-stack {
      display: grid;
      gap: 12px;
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
      max-height: 106px;
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
      gap: 8px;
      max-height: 210px;
      overflow-y: auto;
    }
    .list-item {
      border: 1px solid #e2e8f0;
      background: var(--panel-soft);
      border-radius: 8px;
      padding: 10px;
      line-height: 1.5;
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
    .interaction-card {
      border-left: 3px solid var(--blue);
    }
    .interaction-message {
      margin: 0;
      line-height: 1.6;
      color: #1f2937;
      font-size: 14px;
    }
    .interaction-message.failed {
      color: #b91c1c;
    }
    .settings {
      margin: 16px 0;
    }
    details > summary {
      cursor: pointer;
      color: var(--muted);
      font-size: 13px;
      user-select: none;
    }
    .config-grid {
      display: grid;
      grid-template-columns: 1fr 1fr 1fr auto;
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
      .workspace { grid-template-columns: 1fr; }
      .transcript-box { height: 420px; }
      .config-grid, .mock-row, .debug-grid, .intelligence-toolbar { grid-template-columns: 1fr; }
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

  <details class="panel settings">
    <summary>LLM 配置</summary>
    <div class="config-grid">
      <label class="field">Base URL
        <input id="llmBaseURL" placeholder="https://api.example.com/v1">
      </label>
      <label class="field">API Key
        <input id="llmAPIKey" type="password" placeholder="留空表示保留已保存 Key">
      </label>
      <label class="field">Model
        <input id="llmModel" placeholder="model-name">
      </label>
      <button onclick="saveLLMConfig()">保存配置</button>
    </div>
    <p class="notice" id="llmConfigStatus">LLM 配置未加载</p>
  </details>

  <div class="workspace">
    <section class="panel">
      <h2>全文转写</h2>
      <div id="fullTranscript" class="transcript-box">
        <span class="placeholder">等待转写内容...</span>
      </div>
    </section>

    <aside class="intelligence-stack">
      <section class="panel">
        <div class="intelligence-toolbar">
          <div class="metric-row">
            <div class="metric"><strong id="summaryCount">0</strong><span>摘要版本</span></div>
            <div class="metric"><strong id="decisionCount">0</strong><span>决策</span></div>
            <div class="metric"><strong id="actionCount">0</strong><span>待办</span></div>
          </div>
          <button onclick="analyzeNow()">立即结构化</button>
        </div>
        <h2>会议摘要</h2>
        <div id="summaryBox" class="summary-text placeholder">暂无摘要</div>
      </section>

      <section class="panel">
        <h2>待办事项</h2>
        <ol id="actionItemsList" class="list"></ol>
      </section>

      <section class="panel">
        <h2>会议决策</h2>
        <ol id="decisionsList" class="list"></ol>
      </section>

      <section class="panel interaction-card">
        <h2>语音交互</h2>
        <p id="interactionMessage" class="interaction-message placeholder">等待语音指令...</p>
      </section>
    </aside>
  </div>

  <details class="panel settings">
    <summary>调试数据</summary>
    <div class="debug-grid">
      <section>
        <h2>状态事件</h2>
        <pre id="events">[]</pre>
      </section>
      <section>
        <h2>转写原始数据</h2>
        <pre id="utterances">[]</pre>
      </section>
    </div>
    <section style="margin-top: 12px;">
      <h2>结构化原始数据</h2>
      <pre id="intelligenceRaw">null</pre>
    </section>
    <section style="margin-top: 12px;">
      <h2>交互事件</h2>
      <pre id="interactionsRaw">[]</pre>
    </section>
  </details>
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
  if (currentMeetingId) refreshLists();
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

async function loadLLMConfig() {
  const res = await fetch("/api/llm-config");
  const cfg = await res.json().catch(() => null);
  if (!cfg) return;
  document.getElementById("llmBaseURL").value = cfg.baseUrl || "";
  document.getElementById("llmModel").value = cfg.model || "";
  document.getElementById("llmAPIKey").value = "";
  document.getElementById("llmConfigStatus").textContent =
    "当前模型：" + (cfg.model || "未配置") + "，Key：" + (cfg.hasApiKey ? "已保存" : "未保存");
}

async function saveLLMConfig() {
  const res = await fetch("/api/llm-config", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      baseUrl: document.getElementById("llmBaseURL").value,
      apiKey: document.getElementById("llmAPIKey").value,
      model: document.getElementById("llmModel").value
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

  renderList(decisionsList, decisions, (item, index) => {
    return '<span class="item-index">' + (index + 1) + '</span>' + escapeHtml(item.content || "");
  });
  renderList(actionItemsList, actions, (item, index) => {
    const owner = item.owner || "负责人待定";
    const due = item.dueDate || "时间待定";
    return '<span class="item-index">' + (index + 1) + '</span>' +
      escapeHtml(item.task || "") +
      '<div class="item-meta">' + escapeHtml(owner + " / " + due + " / " + (item.status || "open")) + '</div>';
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
  if (!Array.isArray(events) || !events.length) {
    renderInteractionMessage(null);
    return;
  }
  renderInteractionMessage(events[events.length - 1]);
}

function renderInteractionUpdate(update) {
  if (update && update.event) renderInteractionMessage(update.event);
  if (update && update.snapshot) renderIntelligence(update.snapshot);
  refreshLists();
}

function renderInteractionMessage(event) {
  const message = document.getElementById("interactionMessage");
  if (!message) return;
  if (!event) {
    message.textContent = "等待语音指令...";
    message.className = "interaction-message placeholder";
    return;
  }
  const ok = event.executionStatus === "success";
  message.textContent = event.message || event.errorMessage || "已收到语音指令。";
  message.className = ok ? "interaction-message" : "interaction-message failed";
}

function renderList(el, items, formatter) {
  el.innerHTML = "";
  if (!Array.isArray(items) || !items.length) {
    const li = document.createElement("li");
    li.className = "list-item placeholder";
    li.textContent = "暂无内容";
    el.appendChild(li);
    return;
  }
  items.forEach((item, index) => {
    const li = document.createElement("li");
    li.className = "list-item";
    li.innerHTML = formatter(item, index);
    el.appendChild(li);
  });
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
