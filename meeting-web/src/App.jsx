import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiPath, fetchJSON, wsPath } from "./api.js";

const EMPTY_SNAPSHOT = {
  type: "intelligence_update",
  meetingId: "",
  title: "",
  meetingType: "",
  summary: null,
  decisions: [],
  actionItems: [],
  risks: [],
  openQuestions: [],
  missingFieldAlerts: []
};

const VIEW_LABELS = {
  live: "实时记录",
  summary: "会议总结"
};

const VIEW_HINTS = {
  live: "当前主屏聚焦实时转写和会议摘要，结构化内容仍在后台持续更新。",
  summary: "会议总结页展示待办、决策、风险和待确认问题。"
};

const STATUS_LABELS = {
  standby: "待开始",
  recording: "记录中",
  soft_paused: "已暂停",
  ending: "结束中",
  ended: "已结束"
};

const INTERACTION_STEPS = [
  { key: "received", label: "语音指令识别" },
  { key: "understanding", label: "LLM 理解中" },
  { key: "executing", label: "工具执行中" },
  { key: "completed", label: "执行完成" }
];

const INSIGHT_COLLECTIONS = [
  { key: "actionItems" },
  { key: "decisions" },
  { key: "risks" },
  { key: "openQuestions" }
];

function parseRouteMeetingID() {
  const path = window.location.pathname.replace(/\/+$/, "");
  if (path.startsWith("/meeting/")) {
    return decodeURIComponent(path.slice("/meeting/".length));
  }
  return "";
}

function visibleItems(items) {
  return Array.isArray(items) ? items.filter((item) => !item.isDeleted) : [];
}

function normalizeSnapshot(snapshot) {
  const merged = { ...EMPTY_SNAPSHOT, ...(snapshot || {}) };
  return {
    ...merged,
    decisions: visibleItems(merged.decisions),
    actionItems: visibleItems(merged.actionItems),
    risks: visibleItems(merged.risks),
    openQuestions: visibleItems(merged.openQuestions),
    missingFieldAlerts: Array.isArray(merged.missingFieldAlerts) ? merged.missingFieldAlerts : []
  };
}

function formatDuration(seconds) {
  const value = Math.max(0, Number(seconds) || 0);
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  const secs = value % 60;
  if (hours > 0) {
    return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
  }
  return `${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
}

function meetingTitle(meeting, snapshot) {
  return snapshot.title || meeting?.title || "未命名会议";
}

function meetingType(meeting, snapshot) {
  return snapshot.meetingType || meeting?.meetingType || meeting?.type || "general";
}

function itemMissingText(fields) {
  if (!Array.isArray(fields) || fields.length === 0) return "";
  const labels = {
    owner: "缺负责人",
    due_date: "缺截止时间",
    mitigation: "缺处理方案",
    status: "缺状态"
  };
  return fields.map((field) => labels[field] || field).join("、");
}

function hasMissingFields(item) {
  return Array.isArray(item?.missingFields) && item.missingFields.length > 0;
}

function itemMotionKey(collectionKey, item) {
  return `${collectionKey}:${item?.id || item?.__motionKey || ""}`;
}

function stableItemSignature(item) {
  return JSON.stringify({
    task: item?.task || "",
    content: item?.content || "",
    question: item?.question || "",
    owner: item?.owner || "",
    dueDate: item?.dueDate || "",
    status: item?.status || "",
    level: item?.level || "",
    impact: item?.impact || "",
    mitigation: item?.mitigation || "",
    sourceText: item?.sourceText || "",
    missingFields: Array.isArray(item?.missingFields) ? [...item.missingFields].sort() : []
  });
}

function missingSignature(item) {
  return Array.isArray(item?.missingFields) ? [...item.missingFields].sort().join("|") : "";
}

function hasSnapshotContent(snapshot) {
  if (!snapshot) return false;
  if (snapshot.meetingId || snapshot.summary?.content || snapshot.title) return true;
  return INSIGHT_COLLECTIONS.some((collection) => Array.isArray(snapshot[collection.key]) && snapshot[collection.key].length > 0);
}

function motionTypeFromIntent(event, fallback) {
  const intent = event?.intent || "";
  if (/undo/.test(intent)) return "restored";
  if (/merge/.test(intent)) return "merged";
  if (/move|convert/.test(intent)) return "converted";
  if (/complete|close/.test(intent)) return "completed";
  if (/create/.test(intent)) return "created";
  if (/update|rewrite/.test(intent)) return "updated";
  if (/delete/.test(intent)) return "deleting";
  return fallback;
}

function buildAnimatedSnapshot(previous, next, event, animate) {
  const display = { ...next };
  const marks = {};
  let hasGhosts = false;

  if (!animate || !hasSnapshotContent(previous)) {
    return { display: next, marks, hasGhosts };
  }

  INSIGHT_COLLECTIONS.forEach((collection) => {
    const previousItems = visibleItems(previous[collection.key]);
    const nextItems = visibleItems(next[collection.key]);
    const previousByID = new Map(previousItems.filter((item) => item.id).map((item) => [item.id, item]));
    const nextByID = new Map(nextItems.filter((item) => item.id).map((item) => [item.id, item]));
    const displayItems = [...nextItems];

    previousItems.forEach((item, index) => {
      if (!item.id || nextByID.has(item.id)) return;
      const ghost = { ...item, __motionGhost: true };
      displayItems.splice(Math.min(index, displayItems.length), 0, ghost);
      marks[itemMotionKey(collection.key, ghost)] = "deleting";
      hasGhosts = true;
    });

    nextItems.forEach((item) => {
      const key = itemMotionKey(collection.key, item);
      const previousItem = item.id ? previousByID.get(item.id) : null;

      if (!previousItem) {
        const createdMotion = motionTypeFromIntent(event, "created");
        marks[key] = createdMotion === "deleting" ? "created" : createdMotion;
        return;
      }

      const changed = stableItemSignature(previousItem) !== stableItemSignature(item);
      if (!changed) return;

      const missingChanged = missingSignature(previousItem) !== missingSignature(item);
      if (missingChanged && hasMissingFields(item)) {
        marks[key] = "missing";
        return;
      }

      const updateMotion = motionTypeFromIntent(event, "updated");
      marks[key] = updateMotion === "created" || updateMotion === "deleting" ? "updated" : updateMotion;
    });

    display[collection.key] = displayItems;
  });

  return { display, marks, hasGhosts };
}

function inferViewFromInteraction(event) {
  if (!event) return "";
  if (event.intent === "switch_meeting_view") {
    const text = `${event.targetId || ""} ${event.message || ""} ${event.instruction || ""}`;
    if (/live|实时|转写|记录/.test(text)) return "live";
    return "summary";
  }
  return "";
}

function interactionPhase(statusUpdate, event) {
  if (statusUpdate?.status === "executing") return "executing";
  if (statusUpdate?.status === "understanding") return "understanding";
  if (!event) return "";
  return event.executionStatus === "success" ? "completed" : "failed";
}

function buildTranscriptText(utterances) {
  return visibleItems(utterances)
    .filter((item) => item.isFinal !== false)
    .map((item) => (item.text || "").trim())
    .filter(Boolean)
    .join("\n\n");
}

export default function App() {
  const routeMeetingID = useRef(parseRouteMeetingID());
  const currentMeetingID = useRef("");
  const wsReconnectTimer = useRef(0);
  const actualSnapshotRef = useRef(EMPTY_SNAPSHOT);
  const motionTimers = useRef([]);

  const [meeting, setMeeting] = useState(null);
  const [meetingID, setMeetingID] = useState("");
  const [status, setStatus] = useState("standby");
  const [durationSeconds, setDurationSeconds] = useState(0);
  const [utterances, setUtterances] = useState([]);
  const [snapshot, setSnapshot] = useState(EMPTY_SNAPSHOT);
  const [displaySnapshot, setDisplaySnapshot] = useState(EMPTY_SNAPSHOT);
  const [motionMarks, setMotionMarks] = useState({});
  const [interactions, setInteractions] = useState([]);
  const [interactionStatus, setInteractionStatus] = useState(null);
  const [activeView, setActiveViewState] = useState(() => {
    const savedView = localStorage.getItem("meetingDisplayView") || "live";
    return VIEW_LABELS[savedView] ? savedView : "live";
  });
  const [viewVersion, setViewVersion] = useState(0);
  const [shareOpen, setShareOpen] = useState(false);
  const [endConfirmOpen, setEndConfirmOpen] = useState(false);
  const [interactionCollapsed, setInteractionCollapsed] = useState(() => localStorage.getItem("interactionDockCollapsed") !== "false");
  const [loadingMessage, setLoadingMessage] = useState("正在连接会议服务...");
  const [socketState, setSocketState] = useState("connecting");

  const lastInteraction = interactions.length ? interactions[interactions.length - 1] : null;
  const transcriptText = useMemo(() => buildTranscriptText(utterances), [utterances]);
  const phase = interactionPhase(interactionStatus, lastInteraction);
  const title = meetingTitle(meeting, snapshot);
  const type = meetingType(meeting, snapshot);

  const clearMotionTimers = useCallback(() => {
    motionTimers.current.forEach((timer) => window.clearTimeout(timer));
    motionTimers.current = [];
  }, []);

  const applyIntelligenceSnapshot = useCallback((rawSnapshot, event = null, animate = true) => {
    const next = normalizeSnapshot(rawSnapshot);
    const previous = actualSnapshotRef.current || EMPTY_SNAPSHOT;
    const result = buildAnimatedSnapshot(previous, next, event, animate);

    actualSnapshotRef.current = next;
    clearMotionTimers();
    setSnapshot(next);
    setDisplaySnapshot(result.display);
    setMotionMarks(result.marks);

    if (Object.keys(result.marks).length > 0) {
      const timer = window.setTimeout(() => {
        setDisplaySnapshot(next);
        setMotionMarks({});
      }, result.hasGhosts ? 1900 : 2400);
      motionTimers.current = [timer];
    }
  }, [clearMotionTimers]);

  const setActiveView = useCallback((nextView, persist = true) => {
    const safeView = VIEW_LABELS[nextView] ? nextView : "live";
    setActiveViewState((previous) => {
      if (previous !== safeView) {
        setViewVersion((value) => value + 1);
      }
      return safeView;
    });
    if (persist) {
      localStorage.setItem("meetingDisplayView", safeView);
    }
  }, []);

  const acceptMeetingID = useCallback((id) => {
    if (!id) return false;
    if (routeMeetingID.current && id !== routeMeetingID.current) return false;
    currentMeetingID.current = id;
    setMeetingID(id);
    return true;
  }, []);

  const toggleInteractionDock = useCallback(() => {
    setInteractionCollapsed((collapsed) => {
      const next = !collapsed;
      localStorage.setItem("interactionDockCollapsed", String(next));
      return next;
    });
  }, []);

  const endMeeting = useCallback(async () => {
    if (!meetingID) return;
    await fetchJSON(`/api/meetings/${encodeURIComponent(meetingID)}/end`, { method: "POST" });
    setEndConfirmOpen(false);
  }, [meetingID]);

  const loadMeetingData = useCallback(async (id) => {
    if (!id || !acceptMeetingID(id)) return;
    const [utteranceList, intelligence, interactionList] = await Promise.all([
      fetchJSON(`/api/meetings/${encodeURIComponent(id)}/utterances`).catch(() => []),
      fetchJSON(`/api/meetings/${encodeURIComponent(id)}/intelligence`).catch(() => EMPTY_SNAPSHOT),
      fetchJSON(`/api/meetings/${encodeURIComponent(id)}/interactions`).catch(() => [])
    ]);
    setUtterances(visibleItems(utteranceList));
    applyIntelligenceSnapshot(intelligence, null, false);
    setInteractions(Array.isArray(interactionList) ? interactionList : []);
  }, [acceptMeetingID, applyIntelligenceSnapshot]);

  const applyStateUpdate = useCallback((update) => {
    const id = update?.meetingId || update?.meeting?.id || currentMeetingID.current;
    const previousID = currentMeetingID.current;
    if (id && !acceptMeetingID(id)) return;
    if (update?.meeting) {
      setMeeting(update.meeting);
      setStatus(update.meeting.status || update.status || "standby");
      setDurationSeconds(update.meeting.durationSeconds || update.durationSeconds || 0);
    } else {
      setStatus(update?.status || "standby");
      setDurationSeconds(update?.durationSeconds || 0);
    }
    if (id && id !== previousID) {
      loadMeetingData(id);
    }
  }, [acceptMeetingID, loadMeetingData]);

  const hydrate = useCallback(async () => {
    try {
      const routedID = routeMeetingID.current;
      if (routedID) {
        const meetingDetail = await fetchJSON(`/api/meetings/${encodeURIComponent(routedID)}`);
        acceptMeetingID(meetingDetail.id);
        setMeeting(meetingDetail);
        setStatus(meetingDetail.status || "standby");
        setDurationSeconds(meetingDetail.durationSeconds || 0);
        await loadMeetingData(meetingDetail.id);
      } else {
        const state = await fetchJSON("/api/state");
        applyStateUpdate(state);
        const id = state?.meetingId || state?.meeting?.id;
        if (id) {
          await loadMeetingData(id);
        }
      }
      setLoadingMessage("");
    } catch (error) {
      setLoadingMessage(`读取会议数据失败：${error.message}`);
    }
  }, [acceptMeetingID, applyStateUpdate, loadMeetingData]);

  useEffect(() => {
    hydrate();
  }, [hydrate]);

  useEffect(() => () => clearMotionTimers(), [clearMotionTimers]);

  useEffect(() => {
    if (!meeting?.startedAt || status === "ended") return undefined;
    const started = new Date(meeting.startedAt).getTime();
    if (Number.isNaN(started)) return undefined;
    const timer = window.setInterval(() => {
      setDurationSeconds(Math.max(0, Math.floor((Date.now() - started) / 1000)));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [meeting?.startedAt, status]);

  useEffect(() => {
    let stopped = false;
    let socket;

    const connect = () => {
      socket = new WebSocket(wsPath("/ws"));
      setSocketState("connecting");

      socket.onopen = () => {
        if (!stopped) setSocketState("online");
      };

      socket.onmessage = (event) => {
        const message = JSON.parse(event.data);
        const id = message.meetingId || message.meeting?.id || currentMeetingID.current;
        if (id && routeMeetingID.current && id !== routeMeetingID.current) return;

        if (message.type === "state_update") {
          applyStateUpdate(message);
        }

        if (message.type === "transcript_delta" && message.utterance) {
          if (!acceptMeetingID(message.meetingId)) return;
          setUtterances((items) => {
            const next = visibleItems(items).filter((item) => item.id !== message.utterance.id);
            return [...next, message.utterance];
          });
        }

        if (message.type === "intelligence_update") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          applyIntelligenceSnapshot(message, null, true);
        }

        if (message.type === "interaction_status") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setInteractionStatus(message);
        }

        if (message.type === "interaction_update" && message.event) {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setInteractionStatus(null);
          setInteractions((items) => {
            const next = items.filter((item) => item.id !== message.event.id);
            return [...next, message.event];
          });
          if (message.snapshot) {
            applyIntelligenceSnapshot(message.snapshot, message.event, true);
          }
          if (message.event.intent === "show_share_qr") {
            setShareOpen(true);
          }
          if (message.event.intent === "request_end_meeting") {
            setEndConfirmOpen(true);
          }
          const nextView = inferViewFromInteraction(message.event);
          if (nextView) {
            setActiveView(nextView, false);
          }
        }
      };

      socket.onclose = () => {
        if (stopped) return;
        setSocketState("offline");
        wsReconnectTimer.current = window.setTimeout(connect, 1200);
      };
    };

    connect();
    return () => {
      stopped = true;
      window.clearTimeout(wsReconnectTimer.current);
      if (socket) socket.close();
    };
  }, [acceptMeetingID, applyIntelligenceSnapshot, applyStateUpdate, setActiveView]);

  const counts = {
    decisions: snapshot.decisions.length,
    actions: snapshot.actionItems.length,
    risks: snapshot.risks.length,
    questions: snapshot.openQuestions.length
  };

  return (
    <main className="meeting-shell">
      <Header
        title={title}
        type={type}
        meetingID={meetingID}
        status={status}
        duration={durationSeconds}
        socketState={socketState}
        activeView={activeView}
        onViewChange={setActiveView}
        onOpenShare={() => setShareOpen(true)}
      />

      {loadingMessage ? <div className="system-banner">{loadingMessage}</div> : null}

      <section className={`display-stage view-${activeView}`} aria-live="polite">
        {activeView === "live" ? (
          <LiveView
            transcriptText={transcriptText}
            summary={snapshot.summary?.content || ""}
            counts={counts}
            utteranceCount={utterances.length}
          />
        ) : (
          <BookView
            key={`${activeView}-${viewVersion}`}
            view={activeView}
            snapshot={displaySnapshot}
            motionMarks={motionMarks}
          />
        )}
        <InteractionDock
          phase={phase}
          statusUpdate={interactionStatus}
          event={lastInteraction}
          collapsed={interactionCollapsed}
          onToggle={toggleInteractionDock}
        />
      </section>

      <ShareModal
        open={shareOpen}
        meetingID={meetingID}
        title={title}
        onClose={() => setShareOpen(false)}
      />
      <EndMeetingConfirmModal
        open={endConfirmOpen}
        title={title}
        status={status}
        onClose={() => setEndConfirmOpen(false)}
        onConfirm={endMeeting}
      />
    </main>
  );
}

function Header({ title, type, meetingID, status, duration, socketState, activeView, onViewChange, onOpenShare }) {
  return (
    <header className="topbar">
      <div className="identity">
        <span className="eyebrow">AI Meeting Secretary</span>
        <h1>{title}</h1>
        <div className="meta-line">
          <span>{type}</span>
          <span>{meetingID || "未绑定会议"}</span>
        </div>
      </div>

      <div className="status-cluster" aria-label="会议状态">
        <StatusPill label={STATUS_LABELS[status] || status} tone={status === "recording" ? "good" : status === "ended" ? "quiet" : "warn"} />
        <StatusPill label={formatDuration(duration)} tone="time" />
        <StatusPill label={socketState === "online" ? "实时连接" : "连接中"} tone={socketState === "online" ? "good" : "warn"} />
      </div>

      <nav className="view-tabs" aria-label="展示视图">
        {Object.entries(VIEW_LABELS).map(([key, label]) => (
          <button
            key={key}
            type="button"
            className={activeView === key ? "active" : ""}
            aria-pressed={activeView === key}
            onClick={() => onViewChange(key)}
          >
            {label}
          </button>
        ))}
        <button type="button" className="share-trigger" onClick={onOpenShare}>
          二维码
        </button>
      </nav>
    </header>
  );
}

function StatusPill({ label, tone }) {
  return <span className={`status-pill ${tone}`}>{label}</span>;
}

function LiveView({ transcriptText, summary, counts, utteranceCount }) {
  return (
    <div className="live-layout">
      <section className="panel transcript-panel" aria-label="实时转写全文">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Realtime Transcript</p>
            <h2>全文转写</h2>
          </div>
          <span className="count-badge">{utteranceCount} 段</span>
        </div>
        <div className={`transcript-body ${transcriptText ? "" : "placeholder"}`}>
          {transcriptText || "等待会议开始，转写内容会在这里按全文连续展示。"}
        </div>
      </section>

      <aside className="panel summary-panel" aria-label="会议摘要">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Meeting Summary</p>
            <h2>会议摘要</h2>
          </div>
          <span className="pulse-dot" aria-label="结构化提取运行中" />
        </div>
        <div className={`summary-copy ${summary ? "" : "placeholder"}`}>
          {summary || "结构化摘要会在转写积累后自动刷新。"}
        </div>
        <div className="metric-grid" aria-label="结构化结果数量">
          <Metric label="待办" value={counts.actions} />
          <Metric label="决策" value={counts.decisions} />
          <Metric label="风险" value={counts.risks} />
          <Metric label="待确认" value={counts.questions} />
        </div>
        <p className="view-hint">{VIEW_HINTS.live}</p>
      </aside>
    </div>
  );
}

function BookView({ view, snapshot, motionMarks }) {
  return (
    <div className="book-stage">
      <section className="book-page" aria-label={VIEW_LABELS[view]}>
        <div className="book-head">
          <div>
            <p className="panel-kicker">Structured Intelligence</p>
            <h2>{VIEW_LABELS[view]}</h2>
          </div>
          <p>{VIEW_HINTS[view]}</p>
        </div>

        <div className="summary-strip">
          <span>摘要</span>
          <p>{snapshot.summary?.content || "暂无摘要。"}</p>
        </div>

        <div className="insight-grid">
          <InsightList
            title="待办事项"
            collectionKey="actionItems"
            items={snapshot.actionItems}
            empty="暂无待办事项"
            motionMarks={motionMarks}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.task}</strong>
                <small>{item.owner || "负责人待定"} / {item.dueDate || "时间待定"} / {item.status || "open"}</small>
                {itemMissingText(item.missingFields) ? <em>{itemMissingText(item.missingFields)}</em> : null}
              </>
            )}
          />
          <InsightList
            title="会议决策"
            collectionKey="decisions"
            items={snapshot.decisions}
            empty="暂无会议决策"
            motionMarks={motionMarks}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.content}</strong>
                {item.sourceText ? <small>来源：{item.sourceText}</small> : null}
              </>
            )}
          />
          <InsightList
            title="风险与阻塞"
            collectionKey="risks"
            items={snapshot.risks}
            empty="暂无风险与阻塞"
            motionMarks={motionMarks}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.content}</strong>
                <small>{item.level || "medium"} / {item.owner || "负责人待定"} / {item.dueDate || "时间待定"}</small>
                {item.impact ? <small>影响：{item.impact}</small> : null}
                {item.mitigation ? <small>方案：{item.mitigation}</small> : null}
                {itemMissingText(item.missingFields) || (!item.mitigation ? "缺处理方案" : "") ? <em>{itemMissingText(item.missingFields) || "缺处理方案"}</em> : null}
              </>
            )}
          />
          <InsightList
            title="待确认问题"
            collectionKey="openQuestions"
            items={snapshot.openQuestions}
            empty="暂无待确认问题"
            motionMarks={motionMarks}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.question}</strong>
                <small>{item.owner || "跟进人待定"} / {item.dueDate || "确认时间待定"} / {item.status || "open"}</small>
                {itemMissingText(item.missingFields) ? <em>{itemMissingText(item.missingFields)}</em> : null}
              </>
            )}
          />
        </div>

      </section>
    </div>
  );
}

function Metric({ label, value }) {
  return (
    <div className="metric">
      <strong>{value}</strong>
      <span>{label}</span>
    </div>
  );
}

function InsightList({ title, collectionKey, items, empty, motionMarks, renderItem }) {
  return (
    <section className="insight-list">
      <div className="insight-head">
        <h3>{title}</h3>
        <span>{items.length}</span>
      </div>
      <div className="insight-items">
        {items.length ? items.map((item, index) => (
          <article
            className={[
              "insight-item",
              item.__motionGhost ? "motion-ghost" : "",
              hasMissingFields(item) ? "has-missing" : "",
              motionMarks?.[itemMotionKey(collectionKey, item)] ? `motion-${motionMarks[itemMotionKey(collectionKey, item)]}` : ""
            ].filter(Boolean).join(" ")}
            key={item.id || `${title}-${index}`}
          >
            {renderItem(item, index)}
          </article>
        )) : <p className="empty-state">{empty}</p>}
      </div>
    </section>
  );
}

function InteractionDock({ phase, statusUpdate, event, collapsed, onToggle }) {
  const instruction = event?.instruction || event?.rawTranscript || statusUpdate?.message || "等待“小艾小艾”语音指令";
  const failed = phase === "failed";
  const message = failed ? (event?.errorMessage || event?.message || "执行失败") : (event?.message || statusUpdate?.message || "语音交互模块待命中。");
  const total = event?.durationMs ? `${event.durationMs} ms` : "";

  return (
    <aside className={`interaction-dock ${phase ? "active" : ""} ${failed ? "failed" : ""} ${collapsed ? "collapsed" : ""}`} aria-label="语音交互状态">
      <button type="button" className="dock-toggle" onClick={onToggle} aria-label={collapsed ? "展开语音交互侧栏" : "收起语音交互侧栏"}>
        {collapsed ? (
          <span className="collapsed-dock-content">
            <span className="collapsed-dots" aria-hidden="true">
              {INTERACTION_STEPS.map((step) => (
                <span key={step.key} className={`collapsed-dot ${stepClass(step.key, phase)}`} />
              ))}
            </span>
            <span className="collapsed-label">语音</span>
          </span>
        ) : "收"}
      </button>
      <div className="dock-head">
        <span>语音交互</span>
        <strong>{failed ? "失败" : phase === "completed" ? "完成" : phase ? "处理中" : "待命"}</strong>
      </div>
      <div className="step-list">
        {INTERACTION_STEPS.map((step) => (
          <div
            key={step.key}
            className={`step ${stepClass(step.key, phase)}`}
          >
            <span />
            <p>{step.label}</p>
          </div>
        ))}
      </div>
      <div className="instruction-card">
        <span>识别到的指令</span>
        <p>{instruction}</p>
      </div>
      <div className="interaction-result">
        <p>{message}</p>
        {total ? <small>总耗时：{total}</small> : null}
      </div>
    </aside>
  );
}

function stepClass(step, phase) {
  const order = ["received", "understanding", "executing", "completed"];
  if (!phase) return "";
  if (phase === "failed") {
    return step === "completed" ? "failed" : "done";
  }
  const current = order.indexOf(phase);
  const index = order.indexOf(step);
  if (index < current) return "done";
  if (index === current) return "current";
  return "";
}

function ShareModal({ open, meetingID, title, onClose }) {
  if (!open) return null;
  const qrURL = meetingID ? apiPath(`/api/meetings/${encodeURIComponent(meetingID)}/share-qr.png?t=${Date.now()}`) : "";
  const minutesURL = meetingID ? apiPath(`/api/meetings/${encodeURIComponent(meetingID)}/minutes.md`) : "";
  const shareURL = meetingID ? apiPath(`/share/${encodeURIComponent(meetingID)}`) : "";

  return (
    <div className="modal-layer" role="dialog" aria-modal="true" aria-label="会议纪要二维码">
      <div className="modal-card">
        <button className="modal-close" type="button" onClick={onClose} aria-label="关闭二维码弹窗">关闭</button>
        <p className="panel-kicker">Minutes Download</p>
        <h2>{title}</h2>
        {meetingID ? (
          <>
            <img className="qr-code" src={qrURL} alt="会议纪要下载二维码" />
            <div className="modal-actions">
              <a href={minutesURL} target="_blank" rel="noreferrer">下载 Markdown</a>
              <a href={shareURL} target="_blank" rel="noreferrer">打开分享页</a>
            </div>
          </>
        ) : (
          <p className="empty-state">当前还没有会议，创建会议后会生成二维码。</p>
        )}
      </div>
    </div>
  );
}

function EndMeetingConfirmModal({ open, title, status, onClose, onConfirm }) {
  if (!open) return null;
  const alreadyEnded = status === "ended";

  return (
    <div className="modal-layer" role="dialog" aria-modal="true" aria-label="确认结束会议">
      <div className="modal-card confirm-card">
        <button className="modal-close" type="button" onClick={onClose} aria-label="关闭确认弹窗">关闭</button>
        <p className="panel-kicker">End Meeting</p>
        <h2>确认结束会议？</h2>
        <p className="confirm-copy">
          {alreadyEnded ? "会议已经结束，无需重复操作。" : `将结束“${title}”，停止记录并触发最终结构化纪要。`}
        </p>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button type="button" className="danger-action" onClick={onConfirm} disabled={alreadyEnded}>
            确认结束
          </button>
        </div>
      </div>
    </div>
  );
}
