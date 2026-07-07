import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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
  frameworkSections: [],
  dynamicSections: [],
  missingFieldAlerts: []
};

const VIEW_LABELS = {
  live: "实时记录",
  summary: "会议总结"
};

const VIEW_HINTS = {
  live: "当前主屏聚焦实时转写和会议摘要，结构化内容仍在后台持续更新。",
  summary: "会议总结页展示待办、决策、风险、待确认问题和按需发现的动态模块。"
};

const MEETING_FRAMEWORK_UI_ENABLED = false;

const MEETING_TYPE_OPTIONS = [
  { value: "project_followup", label: "项目推进会" },
  { value: "exploration", label: "探索讨论会" },
  { value: "retrospective", label: "复盘总结会" },
  { value: "general", label: "普通会议" }
];

const STATUS_LABELS = {
  standby: "待开始",
  recording: "记录中",
  soft_paused: "已暂停",
  ending: "结束中",
  ended: "已结束"
};

const INTERACTION_STEPS = [
  { key: "received", label: "指令识别" },
  { key: "understanding", label: "LLM 理解" },
  { key: "executing", label: "工具执行" },
  { key: "completed", label: "完成" }
];

const ITEM_MOTION_MS = 9600;
const DELETE_MOTION_MS = 5200;
const DELETE_FILL_MOTION_MS = 980;
const MODAL_TRANSITION_MS = 420;
const TARGET_AUDIO_SAMPLE_RATE = 16000;
const SPEECH_PEAK_THRESHOLD = 900;
const TRANSCRIPT_DONE_VISIBLE_MS = 1500;
const INTERACTION_DONE_VISIBLE_MS = 6500;

const INSIGHT_COLLECTIONS = [
  ...(MEETING_FRAMEWORK_UI_ENABLED ? [{ key: "frameworkSections" }] : []),
  { key: "actionItems" },
  { key: "decisions" },
  { key: "risks" },
  { key: "openQuestions" },
  { key: "dynamicSections" }
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
    summary: normalizeSummary(merged.summary),
    decisions: visibleItems(merged.decisions),
    actionItems: visibleItems(merged.actionItems),
    risks: visibleItems(merged.risks),
    openQuestions: visibleItems(merged.openQuestions),
    frameworkSections: normalizeFrameworkSections(merged.frameworkSections),
    dynamicSections: normalizeDynamicSections(merged.dynamicSections),
    missingFieldAlerts: Array.isArray(merged.missingFieldAlerts) ? merged.missingFieldAlerts : []
  };
}

function normalizeSummary(summary) {
  if (!summary) return null;
  const keyPoints = Array.isArray(summary.keyPoints)
    ? summary.keyPoints
    : Array.isArray(summary.key_points)
      ? summary.key_points
      : [];
  const changes = Array.isArray(summary.changes) ? summary.changes : [];
  const content = String(summary.content || "").trim();
  const brief = String(summary.brief || content || "").trim();
  return {
    ...summary,
    content,
    brief,
    background: String(summary.background || "").trim(),
    keyPoints: keyPoints.map((item) => String(item || "").trim()).filter(Boolean),
    progress: String(summary.progress || "").trim(),
    changes: changes.map((item) => String(item || "").trim()).filter(Boolean),
    conclusion: String(summary.conclusion || "").trim(),
    nextFollowUp: String(summary.nextFollowUp || summary.next_follow_up || "").trim()
  };
}

function summaryText(summary) {
  if (!summary) return "";
  return summary.brief || summary.content || summary.conclusion || summary.progress || summary.keyPoints?.[0] || "";
}

function hasStructuredSummary(summary) {
  if (!summary) return false;
  return Boolean(
    summary.background ||
    summary.progress ||
    summary.conclusion ||
    summary.nextFollowUp ||
    (Array.isArray(summary.keyPoints) && summary.keyPoints.length) ||
    (Array.isArray(summary.changes) && summary.changes.length)
  );
}

function normalizeFrameworkSections(sections) {
  return visibleItems(sections).map((section) => ({
    ...section,
    content: String(section?.content || "").trim()
  }));
}

function normalizeDynamicSections(sections) {
  return visibleItems(sections)
    .filter((section) => {
      const visibility = String(section?.visibility || "").trim().toLowerCase();
      return visibility === "visible" || visibility === "pinned";
    })
    .map((section) => ({
      ...section,
      items: visibleItems(section?.items)
    }));
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

function formatCompactDuration(ms) {
  const value = Math.max(0, Number(ms) || 0);
  if (value < 1000) return `${Math.round(value)}ms`;
  const seconds = value / 1000;
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s`;
}

function parseTimeMs(value) {
  const time = Date.parse(value || "");
  return Number.isNaN(time) ? 0 : time;
}

function interactionEventTime(event) {
  return parseTimeMs(event?.completedAt || event?.createdAt);
}

function compactStatusText(value, maxChars = 22) {
  const text = String(value || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find(Boolean) || "";
  const chars = Array.from(text.replace(/\s+/g, " "));
  if (chars.length <= maxChars) return chars.join("");
  return `${chars.slice(0, maxChars).join("")}...`;
}

function normalizeInteractionMode(update) {
  const active = Boolean(update?.active || update?.mode === "editing");
  return {
    mode: active ? "editing" : "normal",
    active,
    expiresAt: String(update?.expiresAt || ""),
    remainingSeconds: Math.max(0, Number(update?.remainingSeconds) || 0),
    reason: String(update?.reason || "")
  };
}

function interactionModeLabel(modeState) {
  if (modeState?.active) {
    const seconds = Math.max(0, Number(modeState.remainingSeconds) || 0);
    return `修改模式 ${seconds}s`;
  }
  return "记录模式";
}

function meetingTitle(meeting, snapshot) {
  return snapshot.title || meeting?.title || "未命名会议";
}

function meetingType(meeting, snapshot) {
  const value = snapshot.meetingType || meeting?.meetingType || meeting?.type || "general";
  return value === "unknown" ? "general" : value;
}

function meetingTypeLabel(value) {
  const normalized = value || "general";
  return MEETING_TYPE_OPTIONS.find((item) => item.value === normalized)?.label || normalized;
}

function shouldShowSourceText(content, sourceText) {
  const source = String(sourceText || "").trim();
  const main = String(content || "").trim();
  if (!source || !main) return Boolean(source);
  const sourceKey = normalizeDisplayCompare(source);
  const mainKey = normalizeDisplayCompare(main);
  if (!sourceKey || !mainKey) return Boolean(source);
  if (sourceKey === mainKey) return false;
  if (sourceKey.includes(mainKey) && sourceKey.length <= mainKey.length + 14) return false;
  if (mainKey.includes(sourceKey) && mainKey.length <= sourceKey.length + 14) return false;
  return true;
}

function normalizeDisplayCompare(value) {
  return String(value || "")
    .toLowerCase()
    .replace(/[\s，。！？、；：,.!?;:"“”'‘’（）()[\]【】《》\-—_/\\|]+/g, "");
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

function isCompletedInsightItem(collectionKey, item) {
  if (collectionKey !== "actionItems") return false;
  const status = String(item?.status || "").trim().toLowerCase();
  return status === "completed" || status === "done" || status === "closed";
}

function itemMotionKey(collectionKey, item) {
  if (collectionKey === "frameworkSections" && item?.sectionKey) {
    return `${collectionKey}:${item.sectionKey}`;
  }
  return `${collectionKey}:${item?.id || item?.__motionKey || ""}`;
}

function collectionItemIdentity(collectionKey, item) {
  if (collectionKey === "frameworkSections" && item?.sectionKey) {
    return item.sectionKey;
  }
  return item?.id || "";
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
    title: item?.title || "",
    description: item?.description || "",
    sectionKey: item?.sectionKey || "",
    items: Array.isArray(item?.items)
      ? item.items.map((entry) => ({
          id: entry?.id || "",
          title: entry?.title || "",
          content: entry?.content || "",
          sourceText: entry?.sourceText || "",
          status: entry?.status || ""
        }))
      : [],
    sourceText: item?.sourceText || "",
    missingFields: Array.isArray(item?.missingFields) ? [...item.missingFields].sort() : []
  });
}

function missingSignature(item) {
  return Array.isArray(item?.missingFields) ? [...item.missingFields].sort().join("|") : "";
}

function snapshotMotionSignature(snapshot) {
  const normalized = normalizeSnapshot(snapshot);
  return JSON.stringify({
    meetingId: normalized.meetingId || "",
    title: normalized.title || "",
    meetingType: normalized.meetingType || "",
    summary: normalized.summary ? {
      brief: normalized.summary.brief || "",
      background: normalized.summary.background || "",
      keyPoints: normalized.summary.keyPoints || [],
      progress: normalized.summary.progress || "",
      changes: normalized.summary.changes || [],
      conclusion: normalized.summary.conclusion || "",
      nextFollowUp: normalized.summary.nextFollowUp || "",
      content: normalized.summary.content || ""
    } : null,
    decisions: normalized.decisions.map((item) => [item.id || "", stableItemSignature(item)]),
    actionItems: normalized.actionItems.map((item) => [item.id || "", stableItemSignature(item)]),
    risks: normalized.risks.map((item) => [item.id || "", stableItemSignature(item)]),
    openQuestions: normalized.openQuestions.map((item) => [item.id || "", stableItemSignature(item)]),
    frameworkSections: normalized.frameworkSections.map((item) => [item.sectionKey || item.id || "", stableItemSignature(item)]),
    dynamicSections: normalized.dynamicSections.map((item) => [item.id || "", stableItemSignature(item)])
  });
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

function convertedDestinationCollectionKey(event) {
  switch (event?.intent || "") {
    case "move_action_item_to_decision":
      return "decisions";
    case "move_decision_to_action_item":
      return "actionItems";
    default:
      return "";
  }
}

function createdMotionForCollection(event, collectionKey) {
  const convertedDestination = convertedDestinationCollectionKey(event);
  if (convertedDestination) {
    return convertedDestination === collectionKey ? "converted" : "created";
  }
  const motion = motionTypeFromIntent(event, "created");
  return motion === "deleting" ? "created" : motion;
}

function interactionCollectionKey(event) {
  const text = `${event?.targetType || ""} ${event?.intent || ""}`;
  if (/dynamic_section|dynamic/.test(text)) return "dynamicSections";
  if (/framework_section|framework/.test(text)) return "frameworkSections";
  if (/risk/.test(text)) return "risks";
  if (/open_question|question/.test(text)) return "openQuestions";
  if (/decision/.test(text)) return "decisions";
  if (/action_item|action/.test(text)) return "actionItems";
  return "";
}

function shouldRevealSummaryForInteraction(event) {
  if (!event || event.executionStatus !== "success") return false;
  if (interactionCollectionKey(event)) return true;
  return /rewrite_summary|update_meeting_title/.test(event.intent || "");
}

function eventTargetsItem(event, item, index) {
  const targetID = event?.targetId || "";
  if (targetID && item?.id === targetID) return true;
  const targetIndex = Number(event?.targetIndex) || 0;
  return targetIndex > 0 && targetIndex === index + 1;
}

function buildAnimatedSnapshot(previous, next, event, animate) {
  const display = { ...next };
  const marks = {};
  let hasGhosts = false;
  const eventCollection = interactionCollectionKey(event);

  if (!animate) {
    return { display: next, marks, hasGhosts };
  }

  INSIGHT_COLLECTIONS.forEach((collection) => {
    const previousItems = visibleItems(previous[collection.key]);
    const nextItems = visibleItems(next[collection.key]);
    const previousByID = new Map(previousItems
      .map((item) => [collectionItemIdentity(collection.key, item), item])
      .filter(([id]) => id));
    const nextByID = new Map(nextItems
      .map((item) => [collectionItemIdentity(collection.key, item), item])
      .filter(([id]) => id));
    const displayItems = [...nextItems];

    previousItems.forEach((item, index) => {
      const identity = collectionItemIdentity(collection.key, item);
      if (!identity || nextByID.has(identity)) return;
      const ghost = { ...item, __motionGhost: true, __motionIndex: index };
      displayItems.splice(Math.min(index, displayItems.length), 0, ghost);
      marks[itemMotionKey(collection.key, ghost)] = "deleting";
      hasGhosts = true;
    });

    nextItems.forEach((item) => {
      const key = itemMotionKey(collection.key, item);
      const identity = collectionItemIdentity(collection.key, item);
      const previousItem = identity ? previousByID.get(identity) : null;

      if (!previousItem) {
        marks[key] = createdMotionForCollection(event, collection.key);
        return;
      }

      if (eventCollection === collection.key && motionTypeFromIntent(event, "updated") === "deleting") {
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

    if (!convertedDestinationCollectionKey(event) && eventCollection === collection.key && event?.executionStatus !== "failed") {
      const eventMotion = motionTypeFromIntent(event, "updated");
      const previousTargets = previousItems
        .map((item, index) => ({ item, index }))
        .filter(({ item, index }) => eventTargetsItem(event, item, index));
      const nextTargets = nextItems
        .map((item, index) => ({ item, index }))
        .filter(({ item, index }) => eventTargetsItem(event, item, index));

      if (eventMotion === "deleting") {
        previousTargets.forEach(({ item, index }) => {
          const existingIndex = displayItems.findIndex((candidate) => collectionItemIdentity(collection.key, candidate) === collectionItemIdentity(collection.key, item));
          const existing = existingIndex >= 0 ? displayItems[existingIndex] : null;
          if (existing) {
            displayItems.splice(existingIndex, 1);
            const ghost = {
              ...existing,
              __motionGhost: true,
              __motionIndex: index,
              __motionState: "deleting"
            };
            displayItems.splice(Math.min(index, displayItems.length), 0, ghost);
            marks[itemMotionKey(collection.key, ghost)] = "deleting";
            hasGhosts = true;
            return;
          }
          if (displayItems.some((candidate) => candidate.__motionGhost && candidate.id === item.id)) return;
          const ghost = {
            ...item,
            __motionGhost: true,
            __motionIndex: index,
            __motionState: "deleting"
          };
          displayItems.splice(Math.min(index, displayItems.length), 0, ghost);
          marks[itemMotionKey(collection.key, ghost)] = "deleting";
          hasGhosts = true;
        });
      } else {
        nextTargets.forEach(({ item }) => {
          const motion = eventMotion === "deleting" ? "updated" : eventMotion;
          marks[itemMotionKey(collection.key, item)] = motion;
        });
      }
    }

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

function timestampToggleFromInteraction(event) {
  if (event?.intent !== "set_transcript_timestamps") return null;
  const text = `${event.targetId || ""} ${event.message || ""} ${event.instruction || ""}`.toLowerCase();
  if (/off|关闭|关掉|隐藏|取消|不要显示/.test(text)) return false;
  if (/on|打开|开启|显示|展示|加上|带上/.test(text)) return true;
  return null;
}

function summaryScrollActionFromInteraction(event) {
  if (event?.intent !== "scroll_summary") return "";
  const text = `${event.targetId || ""} ${event.message || ""} ${event.instruction || ""}`.toLowerCase();
  if (/stop|停止|停下|不要动/.test(text)) return "stop";
  if (/down|上滑|向下|往下|下滚/.test(text)) return "down";
  if (/up|下滑|向上|往上|上滚/.test(text)) return "up";
  return "";
}

function interactionPhase(statusUpdate, event) {
  if (statusUpdate?.status === "executing") return "executing";
  if (statusUpdate?.status === "understanding") return "understanding";
  if (!event) return "";
  return event.executionStatus === "success" ? "completed" : "failed";
}

function visibleInteractionPhase(statusUpdate, event, now) {
  if (statusUpdate?.status) {
    return interactionPhase(statusUpdate, null) || "received";
  }
  if (!event) return "";
  const eventTime = interactionEventTime(event);
  if (!eventTime || now - eventTime > INTERACTION_DONE_VISIBLE_MS) return "";
  return interactionPhase(null, event);
}

function buildAssistantStatus({ meetingStatus, micState, lastTranscriptAt, interactionStatus, lastInteraction, phase, now }) {
  const isListeningMode = meetingStatus === "standby" || meetingStatus === "recording";
  const micConnected = micState?.status === "connected";
  const micError = micState?.status === "error";
  const recentTranscriptDone = lastTranscriptAt > 0 && now - lastTranscriptAt < TRANSCRIPT_DONE_VISIBLE_MS;
  const hasVoice = micConnected && Number(micState?.peak || 0) > SPEECH_PEAK_THRESHOLD;

  let asr = { label: "未监听", tone: "idle", model: "本地 ASR" };
  if (micError) {
    asr = { label: "音频异常", tone: "failed", model: "本地 ASR" };
  } else if (phase === "understanding") {
    asr = { label: "识别中", tone: "recognizing", model: "SenseVoice" };
  } else if (phase) {
    asr = { label: "已完成", tone: "completed", model: "SenseVoice" };
  } else if (recentTranscriptDone) {
    asr = { label: "已完成", tone: "completed", model: "已写入转写" };
  } else if (isListeningMode && hasVoice) {
    asr = { label: "检测到语音", tone: "speaking", model: "本地 ASR" };
  } else if (isListeningMode && micConnected) {
    asr = { label: "监听中", tone: "listening", model: "本地 ASR" };
  } else if (isListeningMode) {
    asr = { label: "等待麦克风", tone: "idle", model: "本地 ASR" };
  } else if (meetingStatus === "ended") {
    asr = { label: "已结束", tone: "idle", model: "本地 ASR" };
  }

  let assistant = { label: "会议助手待命", tone: "idle" };
  if (phase === "understanding") {
    assistant = { label: "会议助手：理解中", tone: "running" };
  } else if (phase === "executing") {
    assistant = { label: "会议助手：工具执行中", tone: "running" };
  } else if (phase === "completed") {
    const cost = lastInteraction?.durationMs ? `，用时 ${formatCompactDuration(lastInteraction.durationMs)}` : "";
    assistant = { label: `会议助手：已完成${cost}`, tone: "completed" };
  } else if (phase === "failed") {
    const reason = compactStatusText(lastInteraction?.errorMessage || lastInteraction?.message || interactionStatus?.message || "未找到目标", 12);
    assistant = { label: `会议助手：失败，${reason}`, tone: "failed" };
  }

  return { asr, assistant, phase };
}

function buildTranscriptRows(utterances) {
  const rows = [];
  for (const item of visibleItems(utterances)) {
    if (item?.isFinal === false) continue;
    const text = normalizeTranscriptText(item?.text || "");
    if (!text) continue;
    const speakerLabel = String(item?.speakerLabel || "speaker_1").trim() || "speaker_1";
    const last = rows[rows.length - 1];
    if (last && last.speakerLabel === speakerLabel) {
      if (text.startsWith(last.text)) {
        last.text = text;
      } else if (!last.text.startsWith(text)) {
        last.text = `${last.text}${needsLeadingSpace(last.text, text) ? " " : ""}${text}`;
      }
      last.id = item.id || last.id;
      last.endedAt = item.endedAt || item.createdAt || last.endedAt;
      last.createdAt = item.createdAt || last.createdAt;
      continue;
    }
    rows.push({
      id: item.id || `${speakerLabel}-${rows.length}`,
      speakerLabel,
      speakerName: speakerDisplayName(speakerLabel),
      startedAt: item.startedAt || item.createdAt || "",
      endedAt: item.endedAt || item.createdAt || "",
      createdAt: item.createdAt || "",
      text
    });
  }
  return rows;
}

function formatTranscriptTimestamp(row) {
  const value = row?.startedAt || row?.createdAt || "";
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  });
}

function speakerDisplayName(label) {
  const normalized = String(label || "").trim();
  const match = normalized.match(/^speaker[_-]?(\d+)$/i);
  if (match) return `发言人 ${match[1]}`;
  if (!normalized || normalized === "unknown") return "未知发言人";
  return normalized;
}

function speakerTone(label) {
  const normalized = String(label || "").trim();
  const match = normalized.match(/(\d+)/);
  const value = match ? Number(match[1]) : Array.from(normalized).reduce((sum, char) => sum + char.charCodeAt(0), 0);
  return `speaker-tone-${Math.abs(value || 1) % 6}`;
}

function rebuildArticleTranscript(utterances) {
  const completed = [];
  let currentText = "";
  let currentItem = null;
  let lastLen = 0;

  for (const item of utterances) {
    if (item?.isFinal === false) continue;
    const text = normalizeTranscriptText(item?.text || "");
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

  segments.push(needsLeadingSpace(last, normalized) ? ` ${normalized}` : normalized);
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
  while (i < limit && a[i] === b[i]) i += 1;
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

function stripMotionFields(item) {
  const {
    __motionBatch,
    __motionGhost,
    __motionIndex,
    __motionKey,
    __motionState,
    ...clean
  } = item || {};
  return clean;
}

function insightItemID(collectionKey, item, index) {
  if (collectionKey === "frameworkSections" && item?.sectionKey) {
    return `${collectionKey}-${item.sectionKey}`;
  }
  return item?.id || item?.__motionKey || `${collectionKey}-${index}-${stableItemSignature(item)}`;
}

function useAnimatedInsightItems(items, collectionKey, motionMarks, motionRun) {
  const [animatedItems, setAnimatedItems] = useState([]);
  const previousItems = useRef(new Map());
  const initialized = useRef(false);
  const cleanupTimers = useRef([]);

  useEffect(() => () => {
    cleanupTimers.current.forEach((timer) => window.clearTimeout(timer));
  }, []);

  useEffect(() => {
    cleanupTimers.current.forEach((timer) => window.clearTimeout(timer));
    cleanupTimers.current = [];

    const rawItems = Array.isArray(items) ? items : [];
    const nextItems = rawItems.filter((item) => !item.__motionGhost);
    const incomingGhosts = rawItems.filter((item) => item.__motionGhost);
    const nextIDs = new Set(nextItems.map((item, index) => insightItemID(collectionKey, item, index)));
    const incomingGhostIDs = new Set(incomingGhosts.map((item, index) => insightItemID(collectionKey, item, item.__motionIndex ?? index)));
    const previous = previousItems.current;
    const nextDisplay = nextItems.map((item, index) => {
      const id = insightItemID(collectionKey, item, index);
      const previousItem = previous.get(id);
      const explicitMotion = motionMarks?.[itemMotionKey(collectionKey, item)] || "";
      let motionState = explicitMotion;

      if (initialized.current && !previousItem) {
        motionState = motionState || "created";
      } else if (initialized.current && previousItem && stableItemSignature(previousItem) !== stableItemSignature(item)) {
        motionState = motionState || "updated";
      }

      return {
        ...item,
        __motionKey: id,
        __motionState: motionState,
        __motionBatch: motionState ? motionRun : 0
      };
    });

    incomingGhosts.forEach((item, index) => {
      const id = insightItemID(collectionKey, item, item.__motionIndex ?? index);
      if (nextIDs.has(id)) return;
      nextDisplay.splice(Math.min(item.__motionIndex ?? index, nextDisplay.length), 0, {
        ...item,
        __motionKey: id,
        __motionGhost: true,
        __motionState: item.__motionState || "deleting",
        __motionBatch: motionRun || item.__motionBatch || 1
      });
    });

    Array.from(previous.entries()).forEach(([id, item], previousIndex) => {
      if (nextIDs.has(id)) return;
      if (incomingGhostIDs.has(id)) return;
      nextDisplay.splice(Math.min(previousIndex, nextDisplay.length), 0, {
        ...item,
        __motionKey: id,
        __motionGhost: true,
        __motionState: "deleting",
        __motionBatch: motionRun
      });
    });

    previousItems.current = new Map(nextItems.map((item, index) => [
      insightItemID(collectionKey, item, index),
      stripMotionFields(item)
    ]));
    initialized.current = true;
    setAnimatedItems(nextDisplay);

    cleanupTimers.current.push(window.setTimeout(() => {
      setAnimatedItems((current) => current.filter((item) => !item.__motionGhost));
    }, DELETE_MOTION_MS));

    cleanupTimers.current.push(window.setTimeout(() => {
      setAnimatedItems((current) => current.map((item) => ({
        ...item,
        __motionState: "",
        __motionBatch: 0
      })));
    }, ITEM_MOTION_MS));
  }, [collectionKey, items, motionMarks, motionRun]);

  return animatedItems;
}

function useInsightListLayoutMotion(itemsRef, animatedItems, motionRun) {
  const previousRects = useRef(new Map());

  useLayoutEffect(() => {
    const element = itemsRef.current;
    if (!element) return undefined;
    const prefersReducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;

    const previous = previousRects.current;
    const nodes = Array.from(element.querySelectorAll(".insight-item[data-motion-key]"));
    const containerRect = element.getBoundingClientRect();
    const scrollTop = element.scrollTop;
    const ghostNodes = nodes.filter((node) => node.dataset.motionGhost === "true" && node.classList.contains("motion-deleting"));
    const hasDeletingGhost = ghostNodes.length > 0;

    ghostNodes.forEach((node) => {
      const key = node.dataset.motionKey || "";
      const oldRect = previous.get(key);
      const fallbackTop = node.offsetTop;
      const top = oldRect ? oldRect.top - containerRect.top + scrollTop : fallbackTop;
      const height = oldRect ? oldRect.height : node.offsetHeight;
      node.style.position = "absolute";
      node.style.top = `${Math.max(0, top)}px`;
      node.style.left = "10px";
      node.style.right = "10px";
      node.style.width = "auto";
      node.style.height = `${Math.max(1, height)}px`;
      node.style.zIndex = "5";
    });

    if (hasDeletingGhost && !prefersReducedMotion) {
      // 先让删除残影脱离文档流，再计算剩余条目的真实位置，避免列表出现“空位后刷新”。
      element.getBoundingClientRect();
      nodes.forEach((node) => {
        if (node.dataset.motionGhost === "true") return;
        const key = node.dataset.motionKey || "";
        const oldRect = previous.get(key);
        if (!oldRect) return;
        const newRect = node.getBoundingClientRect();
        const deltaX = oldRect.left - newRect.left;
        const deltaY = oldRect.top - newRect.top;
        if (Math.abs(deltaX) < 1 && Math.abs(deltaY) < 1) return;
        const overshootY = Math.abs(deltaY) >= 16 ? (deltaY > 0 ? -4 : 4) : 0;
        node.animate(
          [
            { offset: 0, transform: `translate(${deltaX}px, ${deltaY}px)` },
            { offset: 0.58, transform: `translate(${deltaX * 0.08}px, ${deltaY * 0.18}px)` },
            { offset: 0.82, transform: `translate(0, ${overshootY}px)` },
            { offset: 1, transform: "translate(0, 0)" }
          ],
          {
            duration: DELETE_FILL_MOTION_MS,
            easing: "cubic-bezier(0.16, 1, 0.3, 1)",
            fill: "both"
          }
        );
      });
    }

    previousRects.current = new Map(
      nodes
        .filter((node) => node.dataset.motionGhost !== "true")
        .map((node) => [node.dataset.motionKey || "", node.getBoundingClientRect()])
        .filter(([key]) => key)
    );

    return undefined;
  }, [animatedItems, itemsRef, motionRun]);
}

function usePresence(open, duration = MODAL_TRANSITION_MS) {
  const [rendered, setRendered] = useState(open);
  const [phase, setPhase] = useState(open ? "open" : "closed");

  useEffect(() => {
    let timer = 0;
    let frame = 0;

    if (open) {
      setRendered(true);
      frame = window.requestAnimationFrame(() => setPhase("open"));
    } else if (rendered) {
      setPhase("closing");
      timer = window.setTimeout(() => {
        setRendered(false);
        setPhase("closed");
      }, duration);
    }

    return () => {
      window.clearTimeout(timer);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [duration, open, rendered]);

  return { rendered, phase };
}

function createEmptyAudioState() {
  return {
    ws: null,
    context: null,
    stream: null,
    source: null,
    processor: null,
    meetingID: "",
    active: false
  };
}

function targetSampleCount(inputLength, sourceRate, targetRate) {
  return Math.max(1, Math.round(inputLength * targetRate / sourceRate));
}

function resampleToPCM16(input, sourceRate, targetRate) {
  const outputLength = targetSampleCount(input.length, sourceRate, targetRate);
  const buffer = new ArrayBuffer(outputLength * 2);
  const view = new DataView(buffer);
  const ratio = sourceRate / targetRate;

  for (let index = 0; index < outputLength; index += 1) {
    const sourceIndex = index * ratio;
    const left = Math.floor(sourceIndex);
    const right = Math.min(left + 1, input.length - 1);
    const weight = sourceIndex - left;
    const sample = input[left] * (1 - weight) + input[right] * weight;
    const clamped = Math.max(-1, Math.min(1, sample));
    view.setInt16(index * 2, clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff, true);
  }

  return buffer;
}

function floatPeakToPCM(input) {
  let peak = 0;
  for (let index = 0; index < input.length; index += 1) {
    const value = Math.abs(input[index]);
    if (value > peak) peak = value;
  }
  return Math.round(Math.min(1, peak) * 32767);
}

export default function App() {
  const routeMeetingID = useRef(parseRouteMeetingID());
  const currentMeetingID = useRef("");
  const wsReconnectTimer = useRef(0);
  const actualSnapshotRef = useRef(EMPTY_SNAPSHOT);
  const duplicateSnapshotRef = useRef(null);
  const motionTimers = useRef([]);
  const previousStatusRef = useRef("standby");
  const audioRef = useRef(createEmptyAudioState());
  const summaryPageRef = useRef(null);
  const summaryScrollTimer = useRef(0);

  const [meeting, setMeeting] = useState(null);
  const [meetingID, setMeetingID] = useState("");
  const [status, setStatus] = useState("standby");
  const [durationSeconds, setDurationSeconds] = useState(0);
  const [utterances, setUtterances] = useState([]);
  const [snapshot, setSnapshot] = useState(EMPTY_SNAPSHOT);
  const [displaySnapshot, setDisplaySnapshot] = useState(EMPTY_SNAPSHOT);
  const [motionMarks, setMotionMarks] = useState({});
  const [motionRun, setMotionRun] = useState(0);
  const [interactions, setInteractions] = useState([]);
  const [interactionStatus, setInteractionStatus] = useState(null);
  const [interactionMode, setInteractionMode] = useState(() => normalizeInteractionMode(null));
  const [intelligenceStatus, setIntelligenceStatus] = useState(null);
  const [lastTranscriptAt, setLastTranscriptAt] = useState(0);
  const [assistantClock, setAssistantClock] = useState(() => Date.now());
  const [showTranscriptTimestamps, setShowTranscriptTimestamps] = useState(() => (
    localStorage.getItem("meetingTranscriptShowTimestamps") === "1"
  ));
  const [activeView, setActiveViewState] = useState(() => {
    const savedView = localStorage.getItem("meetingDisplayView") || "live";
    return VIEW_LABELS[savedView] ? savedView : "live";
  });
  const [viewVersion, setViewVersion] = useState(0);
  const [shareOpen, setShareOpen] = useState(false);
  const [endConfirmOpen, setEndConfirmOpen] = useState(false);
  const [hostControlOpen, setHostControlOpen] = useState(false);
  const [hostBusy, setHostBusy] = useState("");
  const [hostError, setHostError] = useState("");
  const [micState, setMicState] = useState({ status: "idle", message: "麦克风未连接", peak: 0, packets: 0 });
  const [meetingTypeSaving, setMeetingTypeSaving] = useState(false);
  const [loadingMessage, setLoadingMessage] = useState("正在连接会议服务...");
  const [socketState, setSocketState] = useState("connecting");

  const lastInteraction = interactions.length ? interactions[interactions.length - 1] : null;
  const transcriptRows = useMemo(() => buildTranscriptRows(utterances), [utterances]);
  const phase = visibleInteractionPhase(interactionStatus, lastInteraction, assistantClock);
  const assistantStatus = useMemo(
    () => buildAssistantStatus({
      meetingStatus: status,
      micState,
      lastTranscriptAt,
      interactionStatus,
      lastInteraction,
      phase,
      now: assistantClock
    }),
    [assistantClock, interactionStatus, lastInteraction, lastTranscriptAt, micState, phase, status]
  );
  const title = meetingTitle(meeting, snapshot);
  const type = meetingType(meeting, snapshot);

  const clearMotionTimers = useCallback(() => {
    motionTimers.current.forEach((timer) => window.clearTimeout(timer));
    motionTimers.current = [];
  }, []);

  const suppressNextDuplicateSnapshot = useCallback((rawSnapshot) => {
    const normalized = normalizeSnapshot(rawSnapshot);
    duplicateSnapshotRef.current = {
      meetingId: normalized.meetingId || "",
      signature: snapshotMotionSignature(normalized),
      until: Date.now() + 2200
    };
  }, []);

  const shouldSkipDuplicateSnapshot = useCallback((rawSnapshot) => {
    const duplicate = duplicateSnapshotRef.current;
    if (!duplicate || Date.now() > duplicate.until) {
      duplicateSnapshotRef.current = null;
      return false;
    }
    const normalized = normalizeSnapshot(rawSnapshot);
    if (duplicate.meetingId && normalized.meetingId && duplicate.meetingId !== normalized.meetingId) {
      return false;
    }
    const sameSnapshot = duplicate.signature === snapshotMotionSignature(normalized);
    if (sameSnapshot) {
      duplicateSnapshotRef.current = null;
    }
    return sameSnapshot;
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

    if (animate && (event || Object.keys(result.marks).length > 0)) {
      setMotionRun((value) => value + 1);
      const timer = window.setTimeout(() => {
        setDisplaySnapshot(next);
        setMotionMarks({});
      }, result.hasGhosts ? DELETE_MOTION_MS : ITEM_MOTION_MS);
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

  const stopSummaryScroll = useCallback(() => {
    if (summaryScrollTimer.current) {
      window.clearInterval(summaryScrollTimer.current);
      summaryScrollTimer.current = 0;
    }
  }, []);

  const startSummaryScroll = useCallback((direction) => {
    const action = direction === "up" ? "up" : direction === "down" ? "down" : "stop";
    if (action === "stop") {
      stopSummaryScroll();
      return;
    }
    setActiveView("summary", false);
    stopSummaryScroll();
    window.setTimeout(() => {
      const element = summaryPageRef.current;
      if (!element) return;
      const delta = action === "up" ? -5 : 5;
      summaryScrollTimer.current = window.setInterval(() => {
        const target = summaryPageRef.current;
        if (!target) {
          stopSummaryScroll();
          return;
        }
        target.scrollBy({ top: delta, behavior: "auto" });
      }, 16);
    }, 80);
  }, [setActiveView, stopSummaryScroll]);

  useEffect(() => {
    localStorage.setItem("meetingTranscriptShowTimestamps", showTranscriptTimestamps ? "1" : "0");
  }, [showTranscriptTimestamps]);

  const acceptMeetingID = useCallback((id) => {
    if (!id) return false;
    if (routeMeetingID.current && id !== routeMeetingID.current) return false;
    currentMeetingID.current = id;
    setMeetingID(id);
    return true;
  }, []);

  const bindRouteToMeeting = useCallback((id) => {
    if (!id) return;
    routeMeetingID.current = id;
    const nextPath = `/meeting/${encodeURIComponent(id)}`;
    const nextURL = `${nextPath}${window.location.search || ""}`;
    if (window.location.pathname !== nextPath) {
      window.history.replaceState(null, "", nextURL);
    }
  }, []);

  const stopMeetingAudio = useCallback((closeSocket = true) => {
    const current = audioRef.current;
    current.active = false;

    const ws = current.ws;
    current.ws = null;
    current.meetingID = "";
    if (closeSocket && ws) {
      try { ws.onclose = null; } catch {}
      try { ws.onerror = null; } catch {}
      if (ws.readyState === WebSocket.OPEN) {
        try { ws.send(JSON.stringify({ type: "end" })); } catch {}
      }
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        try { ws.close(); } catch {}
      }
    }

    if (current.processor) {
      try { current.processor.disconnect(); } catch {}
      current.processor = null;
    }
    if (current.source) {
      try { current.source.disconnect(); } catch {}
      current.source = null;
    }
    if (current.stream) {
      try { current.stream.getTracks().forEach((track) => track.stop()); } catch {}
      current.stream = null;
    }
    if (current.context) {
      try { current.context.close(); } catch {}
      current.context = null;
    }

    setMicState({ status: "idle", message: "麦克风未连接", peak: 0, packets: 0 });
  }, []);

  const startMeetingAudio = useCallback(async (targetMeetingID) => {
    if (!targetMeetingID) {
      throw new Error("请先创建会议。");
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error("当前浏览器不支持麦克风采集。");
    }
    const current = audioRef.current;
    if (current.active && current.ws?.readyState === WebSocket.OPEN && current.meetingID === targetMeetingID) {
      return;
    }

    stopMeetingAudio();
    setMicState({ status: "connecting", message: "正在打开麦克风", peak: 0, packets: 0 });

    const ws = new WebSocket(wsPath(`/api/meetings/${encodeURIComponent(targetMeetingID)}/audio/ws`));
    ws.binaryType = "arraybuffer";
    audioRef.current.ws = ws;
    audioRef.current.meetingID = targetMeetingID;

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        if (message.type === "audio_metrics") {
          setMicState((state) => ({
            ...state,
            status: "connected",
            message: "麦克风已连接",
            peak: Number(message.peak) || state.peak || 0,
            packets: Number(message.packets) || state.packets || 0
          }));
        }
        if (message.type === "audio_blocked") {
          setMicState((state) => ({ ...state, message: "会议未在记录中，音频暂未送入 ASR" }));
        }
      } catch {}
    };
    ws.onclose = () => {
      if (audioRef.current.meetingID === targetMeetingID) {
        stopMeetingAudio(false);
      }
    };
    ws.onerror = () => {
      if (audioRef.current.meetingID === targetMeetingID) {
        setMicState((state) => ({ ...state, status: "error", message: "音频连接异常" }));
      }
    };

    await new Promise((resolve, reject) => {
      ws.onopen = resolve;
      ws.onerror = () => reject(new Error("音频 WebSocket 连接失败"));
    });
    ws.onerror = () => {
      if (audioRef.current.meetingID === targetMeetingID) {
        setMicState((state) => ({ ...state, status: "error", message: "音频连接异常" }));
      }
    };

    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        sampleRate: TARGET_AUDIO_SAMPLE_RATE,
        echoCancellation: false,
        noiseSuppression: false,
        autoGainControl: false
      }
    });
    const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
    if (!AudioContextCtor) {
      stream.getTracks().forEach((track) => track.stop());
      throw new Error("当前浏览器不支持 AudioContext。");
    }

    const context = new AudioContextCtor({ sampleRate: TARGET_AUDIO_SAMPLE_RATE });
    const source = context.createMediaStreamSource(stream);
    const processor = context.createScriptProcessor(1024, 1, 1);
    const sourceRate = context.sampleRate || TARGET_AUDIO_SAMPLE_RATE;
    const audioState = audioRef.current;
    audioState.context = context;
    audioState.stream = stream;
    audioState.source = source;
    audioState.processor = processor;
    audioState.active = true;

    processor.onaudioprocess = (event) => {
      const state = audioRef.current;
      if (!state.active || !state.ws || state.ws.readyState !== WebSocket.OPEN) return;
      const input = event.inputBuffer.getChannelData(0);
      const peak = floatPeakToPCM(input);
      setMicState((previous) => ({ ...previous, status: "connected", message: "麦克风已连接", peak }));
      state.ws.send(resampleToPCM16(input, sourceRate, TARGET_AUDIO_SAMPLE_RATE));
    };

    source.connect(processor);
    processor.connect(context.destination);
    stream.getTracks().forEach((track) => {
      track.enabled = true;
    });
    setMicState((state) => ({ ...state, status: "connected", message: "麦克风已连接" }));
  }, [stopMeetingAudio]);

  const endMeeting = useCallback(async () => {
    if (!meetingID) return;
    setHostBusy("end");
    setHostError("");
    try {
      stopMeetingAudio();
      const update = await fetchJSON(`/api/meetings/${encodeURIComponent(meetingID)}/end`, { method: "POST" });
      const nextStatus = update?.meeting?.status || update?.status || "";
      if (update?.meeting) {
        setMeeting(update.meeting);
        setStatus(nextStatus || "standby");
        setDurationSeconds(update.meeting.durationSeconds || update.durationSeconds || 0);
      }
      setEndConfirmOpen(false);
      if (nextStatus === "ended") {
        setShareOpen(true);
      }
    } catch (error) {
      setHostError(error.message || "结束会议失败");
    } finally {
      setHostBusy("");
    }
  }, [meetingID, stopMeetingAudio]);

  const updateMeetingType = useCallback(async (nextType) => {
    if (!meetingID || !nextType) return;
    setMeetingTypeSaving(true);
    try {
      const result = await fetchJSON(`/api/meetings/${encodeURIComponent(meetingID)}/type`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ meetingType: nextType, locked: true })
      });
      if (result?.meeting) {
        setMeeting(result.meeting);
      }
      if (result?.snapshot) {
        applyIntelligenceSnapshot(result.snapshot, null, true);
      }
    } finally {
      setMeetingTypeSaving(false);
    }
  }, [applyIntelligenceSnapshot, meetingID]);

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

  const createMeetingFromDisplay = useCallback(async () => {
    setHostBusy("create");
    setHostError("");
    try {
      stopMeetingAudio();
      const update = await fetchJSON("/api/meetings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: "展示会议", type: "general", operator: "display" })
      });
      const id = update?.meetingId || update?.meeting?.id || "";
      if (id) {
        bindRouteToMeeting(id);
      }
      applyStateUpdate(update);
      if (id) {
        await loadMeetingData(id);
        await startMeetingAudio(id);
      }
    } catch (error) {
      setHostError(error.message || "创建会议失败");
    } finally {
      setHostBusy("");
    }
  }, [applyStateUpdate, bindRouteToMeeting, loadMeetingData, startMeetingAudio, stopMeetingAudio]);

  const startMeetingFromDisplay = useCallback(async () => {
    if (!meetingID) {
      setHostError("请先创建会议。");
      return;
    }
    setHostBusy("start");
    setHostError("");
    try {
      if (status !== "recording") {
        const update = await fetchJSON(`/api/meetings/${encodeURIComponent(meetingID)}/start`, { method: "POST" });
        applyStateUpdate(update);
      }
      await startMeetingAudio(meetingID);
    } catch (error) {
      stopMeetingAudio();
      setHostError(error.message || "开始会议或打开麦克风失败");
      setMicState((state) => ({ ...state, status: "error", message: "麦克风未开启" }));
    } finally {
      setHostBusy("");
    }
  }, [applyStateUpdate, meetingID, startMeetingAudio, status, stopMeetingAudio]);

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

  useEffect(() => {
    if (!meetingID) return undefined;
    if (status === "standby" || status === "recording") {
      let cancelled = false;
      startMeetingAudio(meetingID).catch((error) => {
        if (cancelled) return;
        setMicState((state) => ({
          ...state,
          status: "error",
          message: error?.message || "麦克风未开启"
        }));
      });
      return () => {
        cancelled = true;
      };
    }
    if (status === "ending" || status === "ended") {
      stopMeetingAudio();
    }
    return undefined;
  }, [meetingID, startMeetingAudio, status, stopMeetingAudio]);

  useEffect(() => {
    if (!lastTranscriptAt) return undefined;
    const remaining = TRANSCRIPT_DONE_VISIBLE_MS - (Date.now() - lastTranscriptAt);
    if (remaining <= 0) return undefined;
    const timer = window.setTimeout(() => setAssistantClock(Date.now()), remaining + 40);
    return () => window.clearTimeout(timer);
  }, [lastTranscriptAt]);

  useEffect(() => {
    if (interactionStatus) {
      setAssistantClock(Date.now());
      return undefined;
    }
    const eventTime = interactionEventTime(lastInteraction);
    if (!eventTime) return undefined;
    const remaining = INTERACTION_DONE_VISIBLE_MS - (Date.now() - eventTime);
    if (remaining <= 0) {
      setAssistantClock(Date.now());
      return undefined;
    }
    const timer = window.setTimeout(() => setAssistantClock(Date.now()), remaining + 40);
    return () => window.clearTimeout(timer);
  }, [interactionStatus, lastInteraction]);

  useEffect(() => () => clearMotionTimers(), [clearMotionTimers]);

  useEffect(() => () => stopSummaryScroll(), [stopSummaryScroll]);

  useEffect(() => {
    if (activeView !== "summary") {
      stopSummaryScroll();
    }
  }, [activeView, stopSummaryScroll]);

  useEffect(() => () => stopMeetingAudio(), [stopMeetingAudio]);

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
    const previousStatus = previousStatusRef.current;
    previousStatusRef.current = status;
    if (!meetingID || status !== "ended" || previousStatus === "ended") return;
    stopMeetingAudio();
    setEndConfirmOpen(false);
    setShareOpen(true);
  }, [meetingID, status, stopMeetingAudio]);

  useEffect(() => {
    if (!interactionMode.active || !interactionMode.expiresAt) return undefined;

    const syncRemaining = () => {
      setInteractionMode((current) => {
        if (!current.active || !current.expiresAt) return current;
        const expiresAt = new Date(current.expiresAt).getTime();
        if (Number.isNaN(expiresAt)) return current;
        const remainingSeconds = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000));
        if (remainingSeconds <= 0) {
          return {
            ...current,
            mode: "normal",
            active: false,
            remainingSeconds: 0,
            reason: current.reason || "修改模式已超时"
          };
        }
        if (remainingSeconds === current.remainingSeconds) return current;
        return { ...current, remainingSeconds };
      });
    };

    syncRemaining();
    const timer = window.setInterval(syncRemaining, 1000);
    return () => window.clearInterval(timer);
  }, [interactionMode.active, interactionMode.expiresAt]);

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
          const now = Date.now();
          setLastTranscriptAt(now);
          setAssistantClock(now);
          setUtterances((items) => {
            const next = visibleItems(items).filter((item) => item.id !== message.utterance.id);
            return [...next, message.utterance];
          });
        }

        if (message.type === "intelligence_update") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          if (shouldSkipDuplicateSnapshot(message)) return;
          setIntelligenceStatus({ status: "completed", message: "结构化分析已完成。", timestamp: message.timestamp || new Date().toISOString() });
          applyIntelligenceSnapshot(message, null, true);
        }

        if (message.type === "intelligence_status") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setIntelligenceStatus({
            status: message.status || "running",
            message: message.message || "结构化状态更新",
            timestamp: message.timestamp || new Date().toISOString()
          });
        }

        if (message.type === "intelligence_error") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setIntelligenceStatus({
            status: "failed",
            message: message.message || "结构化分析失败",
            timestamp: message.timestamp || new Date().toISOString()
          });
        }

        if (message.type === "interaction_status") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setAssistantClock(Date.now());
          setInteractionStatus(message);
        }

        if (message.type === "interaction_mode_update") {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setInteractionMode(normalizeInteractionMode(message));
        }

        if (message.type === "interaction_update" && message.event) {
          if (message.meetingId) acceptMeetingID(message.meetingId);
          setAssistantClock(Date.now());
          setInteractionStatus(null);
          setInteractions((items) => {
            const next = items.filter((item) => item.id !== message.event.id);
            return [...next, message.event];
          });
          if (message.snapshot) {
            if (shouldRevealSummaryForInteraction(message.event)) {
              setActiveView("summary", false);
            }
            suppressNextDuplicateSnapshot(message.snapshot);
            applyIntelligenceSnapshot(message.snapshot, message.event, true);
          }
          if (message.event.intent === "show_share_qr") {
            setShareOpen(true);
          }
          if (message.event.intent === "request_end_meeting") {
            setEndConfirmOpen(true);
          }
          const timestampToggle = timestampToggleFromInteraction(message.event);
          if (timestampToggle !== null) {
            setShowTranscriptTimestamps(timestampToggle);
          }
          const summaryScrollAction = summaryScrollActionFromInteraction(message.event);
          if (summaryScrollAction) {
            startSummaryScroll(summaryScrollAction);
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
  }, [acceptMeetingID, applyIntelligenceSnapshot, applyStateUpdate, setActiveView, shouldSkipDuplicateSnapshot, startSummaryScroll, suppressNextDuplicateSnapshot]);

  const counts = {
    decisions: snapshot.decisions.length,
    actions: snapshot.actionItems.length,
    risks: snapshot.risks.length,
    questions: snapshot.openQuestions.length,
    dynamicSections: snapshot.dynamicSections.length
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
        interactionMode={interactionMode}
        assistantStatus={assistantStatus}
        activeView={activeView}
        typeSaving={meetingTypeSaving}
        onViewChange={setActiveView}
        onTypeChange={updateMeetingType}
        onOpenShare={() => setShareOpen(true)}
      />

      {loadingMessage ? <div className="system-banner">{loadingMessage}</div> : null}

      <section className={`display-stage view-${activeView}`} aria-live="polite">
        <div key={`${activeView}-${viewVersion}`} className={`view-frame view-frame-${activeView}`}>
          {activeView === "live" ? (
            <LiveView
              transcriptRows={transcriptRows}
              summary={snapshot.summary}
              counts={counts}
              utteranceCount={utterances.length}
              intelligenceStatus={intelligenceStatus}
              showTimestamps={showTranscriptTimestamps}
              onToggleTimestamps={setShowTranscriptTimestamps}
            />
          ) : (
            <BookView
              view={activeView}
              snapshot={displaySnapshot}
              meetingType={type}
              intelligenceStatus={intelligenceStatus}
              motionMarks={motionMarks}
              motionRun={motionRun}
              pageRef={summaryPageRef}
            />
          )}
        </div>
        <InteractionDock
          phase={phase}
          statusUpdate={interactionStatus}
          event={lastInteraction}
        />
        <HostControlDock
          open={hostControlOpen}
          meetingID={meetingID}
          status={status}
          busy={hostBusy}
          error={hostError}
          micState={micState}
          onToggle={() => setHostControlOpen((value) => !value)}
          onCreate={createMeetingFromDisplay}
          onStart={startMeetingFromDisplay}
          onRequestEnd={() => setEndConfirmOpen(true)}
          onOpenShare={() => setShareOpen(true)}
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

function Header({ title, type, meetingID, status, duration, socketState, interactionMode, assistantStatus, activeView, typeSaving, onViewChange, onTypeChange, onOpenShare }) {
  const previousTitle = useRef(title);
  const [titleRun, setTitleRun] = useState(0);

  useEffect(() => {
    if (previousTitle.current && previousTitle.current !== title) {
      setTitleRun((value) => value + 1);
    }
    previousTitle.current = title;
  }, [title]);

  return (
    <header className="topbar">
      <div className="identity">
        <span className="eyebrow">AI Meeting Secretary</span>
        <div className="title-row">
          <h1 key={`${title}-${titleRun}`} className={titleRun > 0 ? "title-motion" : ""}>{title}</h1>
          <AssistantStatusBar status={assistantStatus} />
        </div>
        <div className="meta-line">
          {MEETING_FRAMEWORK_UI_ENABLED ? (
            <label className="type-select-wrap">
              <span>类型</span>
              <select value={type} disabled={!meetingID || typeSaving} onChange={(event) => onTypeChange(event.target.value)}>
                {MEETING_TYPE_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
          ) : null}
          <span>{meetingID || "未绑定会议"}</span>
        </div>
      </div>

      <div className="status-cluster" aria-label="会议状态">
        <StatusPill label={STATUS_LABELS[status] || status} tone={status === "recording" ? "good" : status === "ended" ? "quiet" : "warn"} />
        <StatusPill label={formatDuration(duration)} tone="time" />
        <StatusPill label={socketState === "online" ? "实时连接" : "连接中"} tone={socketState === "online" ? "good" : "warn"} />
        <StatusPill
          label={interactionModeLabel(interactionMode)}
          tone={interactionMode?.active ? "editing" : "quiet"}
          title={interactionMode?.active ? "修改模式下语音不会进入全文转写" : "普通会议内容会进入全文转写"}
        />
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

function HostControlDock({ open, meetingID, status, busy, error, micState, onToggle, onCreate, onStart, onRequestEnd, onOpenShare }) {
  const hasMeeting = Boolean(meetingID);
  const recording = status === "recording";
  const ended = status === "ended";
  const standby = status === "standby";
  const ending = status === "ending";
  const micConnected = micState?.status === "connected";
  const micPercent = Math.max(0, Math.min(100, Math.round(((micState?.peak || 0) / 3000) * 100)));

  return (
    <aside className={`host-control ${open ? "open" : "collapsed"} ${busy ? "busy" : ""}`} aria-label="主持控制">
      <button type="button" className="host-control-toggle" onClick={onToggle} aria-expanded={open}>
        主持
      </button>
      <div className="host-control-panel" aria-hidden={!open}>
        <div className="host-control-head">
          <div>
            <p className="panel-kicker">Host Control</p>
            <h2>主持控制</h2>
          </div>
          <span className={`host-state ${recording ? "good" : ended ? "quiet" : "warn"}`}>{STATUS_LABELS[status] || status}</span>
        </div>

        <div className="mic-meter" data-state={micState?.status || "idle"}>
          <div>
            <strong>{micConnected ? "麦克风已开启" : micState?.status === "connecting" ? "正在连接麦克风" : "麦克风未开启"}</strong>
            <span>{micState?.message || "等待开始会议"}</span>
          </div>
          <div className="mic-meter-track" aria-hidden="true">
            <i style={{ width: `${micPercent}%` }} />
          </div>
        </div>

        <div className="host-actions">
          {!hasMeeting ? (
            <button type="button" className="primary-action" disabled={Boolean(busy)} onClick={onCreate}>
              {busy === "create" ? "创建中" : "创建会议"}
            </button>
          ) : null}
          {hasMeeting && (standby || recording) ? (
            <button type="button" className="primary-action" disabled={Boolean(busy)} onClick={onStart}>
              {busy === "start" ? "启动中" : recording && micConnected ? "重新打开麦克风" : "开始会议并打开麦克风"}
            </button>
          ) : null}
          {hasMeeting && recording ? (
            <button type="button" className="danger-action" disabled={Boolean(busy)} onClick={onRequestEnd}>
              结束会议
            </button>
          ) : null}
          {hasMeeting && (ending || ended) ? (
            <button type="button" className="primary-action" disabled={Boolean(busy && !ended)} onClick={onOpenShare}>
              显示二维码
            </button>
          ) : null}
        </div>

        {error ? <p className="host-error">{error}</p> : null}
      </div>
    </aside>
  );
}

function StatusPill({ label, tone, title }) {
  return <span className={`status-pill ${tone || ""}`} title={title || label}>{label}</span>;
}

function AssistantStatusBar({ status }) {
  const asr = status?.asr || { label: "未监听", tone: "idle", model: "本地 ASR" };
  const assistant = status?.assistant || { label: "会议助手待命", tone: "idle" };
  const label = `会议助手状态：${asr.label}，${asr.model}，${assistant.label}`;

  return (
    <div className="assistant-status-bar" aria-label={label} title={label}>
      <span className={`assistant-status-chip assistant-asr ${asr.tone}`}>
        <span className="assistant-status-dot" aria-hidden="true" />
        <span>{asr.label}</span>
      </span>
      <span className="assistant-status-chip assistant-model">{asr.model}</span>
      <span className={`assistant-status-chip assistant-command ${assistant.tone}`}>{assistant.label}</span>
    </div>
  );
}

function LiveView({ transcriptRows, summary, counts, utteranceCount, intelligenceStatus, showTimestamps, onToggleTimestamps }) {
  const transcriptRef = useRef(null);
  const brief = summaryText(summary);
  const hasTranscript = Array.isArray(transcriptRows) && transcriptRows.length > 0;

  useEffect(() => {
    const element = transcriptRef.current;
    if (!element || !hasTranscript) return;
    const frame = window.requestAnimationFrame(() => {
      element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [hasTranscript, transcriptRows, utteranceCount]);

  return (
    <div className="live-layout">
      <section className="panel transcript-panel" aria-label="实时转写全文">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Realtime Transcript</p>
            <h2>全文转写</h2>
          </div>
          <div className="transcript-head-actions">
            <label className="timestamp-toggle">
              <input
                type="checkbox"
                checked={showTimestamps}
                onChange={(event) => onToggleTimestamps(event.target.checked)}
              />
              <span>时间戳</span>
            </label>
            <span className="count-badge">{utteranceCount} 段</span>
          </div>
        </div>
        <div ref={transcriptRef} className={`transcript-body ${hasTranscript ? "" : "placeholder"}`}>
          {hasTranscript ? transcriptRows.map((row) => (
            <article className="transcript-row" key={row.id}>
              <div className="transcript-row-meta">
                <span className={`speaker-badge ${speakerTone(row.speakerLabel)}`}>{row.speakerName}</span>
                {showTimestamps && <time className="transcript-timestamp">{formatTranscriptTimestamp(row)}</time>}
              </div>
              <p>{row.text}</p>
            </article>
          )) : "等待会议开始，转写内容会在这里按全文连续展示。"}
        </div>
      </section>

      <aside className="panel summary-panel" aria-label="会议摘要">
        <div className="panel-head">
          <div>
            <p className="panel-kicker">Meeting Summary</p>
            <h2>会议摘要</h2>
          </div>
          <IntelligenceStatusBadge status={intelligenceStatus} />
        </div>
        <div className={`summary-copy ${brief ? "" : "placeholder"}`}>
          {brief ? <SummaryStructuredView summary={summary} compact /> : "结构化摘要会在转写积累后自动刷新。"}
        </div>
        <div className="metric-grid" aria-label="结构化结果数量">
          <Metric label="待办" value={counts.actions} />
          <Metric label="决策" value={counts.decisions} />
          <Metric label="风险" value={counts.risks} />
          <Metric label="待确认" value={counts.questions} />
          <Metric label="发现" value={counts.dynamicSections} />
        </div>
        <p className="view-hint">{VIEW_HINTS.live}</p>
      </aside>
    </div>
  );
}

function SummaryStrip({ summary }) {
  const brief = summaryText(summary);
  return (
    <section className={`summary-strip ${brief ? "" : "placeholder"}`} aria-label="结构化摘要">
      <div className="summary-strip-head">
        <span>摘要</span>
        <strong>{hasStructuredSummary(summary) ? "结构化" : "实时"}</strong>
      </div>
      {brief ? <SummaryStructuredView summary={summary} /> : <p>暂无摘要。</p>}
    </section>
  );
}

function SummaryStructuredView({ summary, compact = false }) {
  const brief = summaryText(summary);
  const keyPoints = Array.isArray(summary?.keyPoints) ? summary.keyPoints : [];
  const changes = Array.isArray(summary?.changes) ? summary.changes : [];
  return (
    <div className={`summary-structured ${compact ? "compact" : ""}`}>
      {brief ? <p className="summary-brief">{brief}</p> : null}
      <div className="summary-section-grid">
        <SummarySection label="背景" value={summary?.background} />
        <SummarySection label="进展" value={summary?.progress} />
        <SummarySection label="结论" value={summary?.conclusion} />
        <SummarySection label="跟进" value={summary?.nextFollowUp} />
        <SummarySection label="关键要点" values={keyPoints} wide />
        <SummarySection label="重要变化" values={changes} wide />
      </div>
    </div>
  );
}

function SummarySection({ label, value, values, wide = false }) {
  const list = Array.isArray(values) ? values.filter(Boolean) : [];
  const text = String(value || "").trim();
  if (!text && list.length === 0) return null;
  return (
    <div className={`summary-section ${wide ? "wide" : ""}`}>
      <span>{label}</span>
      {list.length ? (
        <ul>
          {list.map((item, index) => <li key={`${label}-${index}`}>{item}</li>)}
        </ul>
      ) : <p>{text}</p>}
    </div>
  );
}

function BookView({ view, snapshot, meetingType, intelligenceStatus, motionMarks, motionRun, pageRef }) {
  return (
    <div className="book-stage">
      <section ref={pageRef} className="book-page" aria-label={VIEW_LABELS[view]}>
        <div className="book-head">
          <div>
            <p className="panel-kicker">Structured Intelligence</p>
            <h2>{VIEW_LABELS[view]}</h2>
            <IntelligenceStatusBadge status={intelligenceStatus} compact />
          </div>
          <p>{VIEW_HINTS[view]}</p>
        </div>

        <SummaryStrip summary={snapshot.summary} />

        {MEETING_FRAMEWORK_UI_ENABLED && snapshot.frameworkSections.length ? (
          <InsightList
            title={`${meetingTypeLabel(meetingType)}记录框架`}
            collectionKey="frameworkSections"
            items={snapshot.frameworkSections}
            empty="暂无会议框架模块"
            motionMarks={motionMarks}
            motionRun={motionRun}
            className="framework-list"
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.title}</strong>
                <p className={`framework-content ${item.content ? "" : "placeholder"}`}>
                  {item.content || "暂未提取到明确内容"}
                </p>
              </>
            )}
          />
        ) : null}

        <div className="insight-grid">
          <InsightList
            title="待办事项"
            collectionKey="actionItems"
            items={snapshot.actionItems}
            empty="暂无待办事项"
            motionMarks={motionMarks}
            motionRun={motionRun}
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
            motionRun={motionRun}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.content}</strong>
                {shouldShowSourceText(item.content, item.sourceText) ? <small>来源：{item.sourceText}</small> : null}
              </>
            )}
          />
          <InsightList
            title="风险与阻塞"
            collectionKey="risks"
            items={snapshot.risks}
            empty="暂无风险与阻塞"
            motionMarks={motionMarks}
            motionRun={motionRun}
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
            motionRun={motionRun}
            renderItem={(item, index) => (
              <>
                <strong>{index + 1}. {item.question}</strong>
                <small>{item.owner || "跟进人待定"} / {item.dueDate || "确认时间待定"} / {item.status || "open"}</small>
                {itemMissingText(item.missingFields) ? <em>{itemMissingText(item.missingFields)}</em> : null}
              </>
            )}
          />
        </div>

        {snapshot.dynamicSections.length ? (
          <InsightList
            title="动态模块"
            collectionKey="dynamicSections"
            items={snapshot.dynamicSections}
            empty="暂无动态模块"
            motionMarks={motionMarks}
            motionRun={motionRun}
            className="dynamic-modules-list"
            renderItem={(section, index) => (
              <DynamicSectionItem section={section} index={index} />
            )}
          />
        ) : null}

      </section>
    </div>
  );
}

function IntelligenceStatusBadge({ status, compact = false }) {
  const current = status?.status || "";
  const message = status?.message || "等待结构化分析";
  const tone = current === "failed" ? "failed" : current === "completed" ? "completed" : current === "running" ? "running" : "idle";
  return (
    <span className={`intelligence-badge ${tone} ${compact ? "compact" : ""}`} title={message}>
      <span className="intelligence-dot" aria-hidden="true" />
      <span>{message}</span>
    </span>
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

function DynamicSectionItem({ section, index }) {
  const entries = Array.isArray(section.items) ? section.items.filter((item) => !item.isDeleted) : [];

  return (
    <>
      <strong>{index + 1}. {section.title || "动态模块"}</strong>
      {section.description ? <small>{section.description}</small> : null}
      <div className="dynamic-module-items">
        {entries.length ? entries.map((entry, itemIndex) => (
          <div className="dynamic-module-entry" key={entry.id || `${section.id || index}-${itemIndex}`}>
            <b>{entry.title || `条目 ${itemIndex + 1}`}</b>
            <span>{entry.content || entry.sourceText || "暂无内容"}</span>
            {entry.sourceText ? <small>来源：{entry.sourceText}</small> : null}
          </div>
        )) : <small>该模块暂无条目。</small>}
      </div>
    </>
  );
}

function InsightList({ title, collectionKey, items, empty, motionMarks, motionRun, renderItem, className = "" }) {
  const sourceItems = Array.isArray(items) ? items : [];
  const animatedItems = useAnimatedInsightItems(sourceItems, collectionKey, motionMarks, motionRun);
  const itemsRef = useRef(null);
  useInsightListLayoutMotion(itemsRef, animatedItems, motionRun);
  const previousVisibleCount = useRef(sourceItems.length);
  const hasMotion = animatedItems.some((item) => item.__motionState);
  const visibleCount = sourceItems.filter((item) => !item.__motionGhost).length;
  const hasNewContentMotion = animatedItems.some((item) => ["created", "converted", "restored"].includes(item.__motionState));
  const motionScrollKey = animatedItems
    .filter((item) => item.__motionState)
    .map((item) => `${item.__motionKey || item.id || ""}:${item.__motionState}:${item.__motionBatch || 0}`)
    .join("|");

  useEffect(() => {
    const element = itemsRef.current;
    const motionTarget = element?.querySelector(
      ".insight-item.motion-created, .insight-item.motion-updated, .insight-item.motion-deleting, .insight-item.motion-completed, .insight-item.motion-merged, .insight-item.motion-converted, .insight-item.motion-restored, .insight-item.motion-missing"
    );
    const shouldScroll = Boolean(motionTarget) || visibleCount > previousVisibleCount.current || hasNewContentMotion;
    previousVisibleCount.current = visibleCount;
    if (!element || !shouldScroll) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const target = motionTarget || element.lastElementChild;
      if (!target) return;
      const top = Math.max(0, target.offsetTop - element.clientHeight * 0.36);
      element.scrollTo({ top, behavior: "smooth" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [hasNewContentMotion, motionRun, motionScrollKey, visibleCount]);

  return (
    <section className={`insight-list ${className} ${hasMotion ? "list-has-motion" : ""}`.trim()}>
      <div className="insight-head">
        <h3>{title}</h3>
        <span>{visibleCount}</span>
      </div>
      <div ref={itemsRef} className="insight-items">
        {animatedItems.length ? animatedItems.map((item, index) => {
          const motionMark = item.__motionState || motionMarks?.[itemMotionKey(collectionKey, item)];
          const motionKey = item.id || `${title}-${index}`;
          const ghostIndex = Number.isFinite(item.__motionIndex) ? item.__motionIndex : index;
          return (
            <article
              className={[
                "insight-item",
                item.__motionGhost ? "motion-ghost" : "",
                isCompletedInsightItem(collectionKey, item) ? "is-completed" : "",
                hasMissingFields(item) ? "has-missing" : "",
                motionMark ? `motion-${motionMark}` : ""
              ].filter(Boolean).join(" ")}
              key={motionMark ? `${item.__motionKey || motionKey}-${motionMark}-${item.__motionBatch || motionRun}` : (item.__motionKey || motionKey)}
              data-motion-key={item.__motionKey || motionKey}
              data-motion-ghost={item.__motionGhost ? "true" : "false"}
              style={item.__motionGhost ? { "--ghost-index": ghostIndex } : undefined}
            >
              {renderItem(item, index)}
            </article>
          );
        }) : <p className="empty-state">{empty}</p>}
      </div>
    </section>
  );
}

function InteractionDock({ phase, statusUpdate, event }) {
  if (!phase) return null;

  const instruction = event?.instruction || event?.rawTranscript || statusUpdate?.message || "等待“会议助手”语音指令";
  const failed = phase === "failed";
  const phaseLabel = failed ? "失败" : phase === "completed" ? "已完成" : phase === "executing" ? "工具执行中" : "理解中";
  const message = failed
    ? compactStatusText(event?.errorMessage || event?.message || "未找到目标", 28)
    : compactStatusText(event?.message || statusUpdate?.message || phaseLabel, 34);
  const total = event?.durationMs ? formatCompactDuration(event.durationMs) : "";
  const currentPhase = phase || "";

  return (
    <aside className={`interaction-dock active ${failed ? "failed" : ""}`} aria-label="语音交互处理进度">
      <div className="dock-head">
        <span>会议助手语音指令</span>
        <strong>{phaseLabel}</strong>
      </div>
      <div className="step-list" aria-label="语音交互处理进度">
        {INTERACTION_STEPS.map((step) => (
          <div
            key={step.key}
            className={`step ${stepClass(step.key, currentPhase)}`}
          >
            <span className="step-node" aria-hidden="true" />
            <strong>{step.label}</strong>
          </div>
        ))}
      </div>
      <div className="interaction-brief" title={`${instruction}\n${message}`}>
        <span>{compactStatusText(instruction, 30)}</span>
        <strong>{message}{total ? ` / ${total}` : ""}</strong>
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
  const presence = usePresence(open);
  if (!presence.rendered) return null;
  const qrURL = meetingID ? apiPath(`/api/meetings/${encodeURIComponent(meetingID)}/share-qr.png?t=${Date.now()}`) : "";
  const minutesURL = meetingID ? apiPath(`/api/meetings/${encodeURIComponent(meetingID)}/minutes.md`) : "";
  const shareURL = meetingID ? apiPath(`/share/${encodeURIComponent(meetingID)}`) : "";

  return (
    <div className={`modal-layer ${presence.phase === "closing" ? "closing" : "open"}`} role="dialog" aria-modal="true" aria-label="会议纪要二维码">
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
  const presence = usePresence(open);
  if (!presence.rendered) return null;
  const alreadyEnded = status === "ended";

  return (
    <div className={`modal-layer ${presence.phase === "closing" ? "closing" : "open"}`} role="dialog" aria-modal="true" aria-label="确认结束会议">
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
