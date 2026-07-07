"""Windows/Ubuntu 开发用 SenseVoice 本地 ASR 服务。

这个脚本实现和 C++ local-asr-service 相同的 HTTP 契约：
  GET  /health
  POST /v1/sessions/{meeting_id}/start
  POST /v1/sessions/{meeting_id}/audio
  POST /v1/sessions/{meeting_id}/flush
  POST /v1/sessions/{meeting_id}/stop

正式 RK3588 部署仍建议使用 C++/sherpa-onnx C API/RKNN；本脚本用于当前
Windows 环境快速验证 SenseVoice-Small 的真实识别效果。
"""

from __future__ import annotations

import argparse
import array
import json
import math
import os
import subprocess
import tempfile
import threading
import time
import wave
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import unquote, urlparse

import sherpa_onnx


CONTRACT_VERSION = "meeting-asr-v1"
PROVIDER = "sensevoice"
SAMPLE_RATE = 16000
BYTES_PER_SAMPLE = 2
FRAME_MS = 20
DEFAULT_SPEAKER = "speaker_1"


@dataclass
class Config:
    host: str = "127.0.0.1"
    port: int = 9001
    model: str = ""
    tokens: str = ""
    language: str = "zh"
    num_threads: int = 4
    provider: str = "cpu"
    use_itn: bool = True
    enable_punctuation: bool = True
    vad_threshold: int = 120
    vad_peak_threshold: int = 900
    vad_noise_ratio: float = 3.0
    end_silence_ms: int = 550
    min_speech_ms: int = 500
    max_segment_ms: int = 8000
    pre_speech_ms: int = 300
    normalize_peak: int = 8000
    max_gain: float = 4.0
    enable_speaker_diarization: bool = True
    campp_model: str = ""
    campp_command: str = ""
    speaker_threshold: float = 0.40
    speaker_sticky_threshold: float = 0.28
    min_speaker_ms: int = 1600


@dataclass
class SpeakerCluster:
    centroids: list[list[float]] = field(default_factory=list)
    counts: list[int] = field(default_factory=list)


@dataclass
class Session:
    pending: bytearray = field(default_factory=bytearray)
    segment: bytearray = field(default_factory=bytearray)
    in_speech: bool = False
    trailing_silence_ms: int = 0
    cursor_ms: int = 0
    segment_start_ms: int = 0
    pre_speech: bytearray = field(default_factory=bytearray)
    noise_floor: float = 80.0
    speaker_cluster: SpeakerCluster = field(default_factory=SpeakerCluster)
    last_speaker_label: str = DEFAULT_SPEAKER


class SenseVoiceEngine:
    def __init__(self, config: Config) -> None:
        started = time.time()
        self.config = config
        self._lock = threading.Lock()
        self._recognizer = sherpa_onnx.OfflineRecognizer.from_sense_voice(
            model=config.model,
            tokens=config.tokens,
            use_itn=config.use_itn,
            num_threads=config.num_threads,
            provider=config.provider,
            language=config.language,
        )
        print(
            "sensevoice recognizer loaded",
            f"model={config.model}",
            f"tokens={config.tokens}",
            f"provider={config.provider}",
            f"threads={config.num_threads}",
            f"cost={time.time() - started:.3f}s",
            flush=True,
        )

    def transcribe(self, pcm: bytes) -> tuple[str, float]:
        samples = pcm16_to_float32(pcm)
        if not samples:
            return "", 0.0
        with self._lock:
            stream = self._recognizer.create_stream()
            stream.accept_waveform(SAMPLE_RATE, samples)
            started = time.time()
            self._recognizer.decode_stream(stream)
            text = restore_punctuation(stream.result.text or "", self.config.enable_punctuation)
            print(f"sensevoice decoded len={len(pcm)} cost={time.time() - started:.3f}s text={text}", flush=True)
        return text, 0.9 if text else 0.0


class SpeakerDiarizer:
    def __init__(self, config: Config) -> None:
        self.config = config
        self.enabled = False
        self._engine: CAMPPEmbeddingEngine | None = None
        if not config.enable_speaker_diarization:
            print("speaker diarization disabled by config", flush=True)
            return

        if config.campp_command:
            self._engine = CommandCAMPPEmbeddingEngine(config.campp_command)
            self.enabled = True
            print(f"CAM++ speaker engine enabled via command: {config.campp_command}", flush=True)
            return

        if config.campp_model:
            try:
                if Path(config.campp_model).exists():
                    self._engine = SherpaCAMPPEmbeddingEngine(config)
                else:
                    self._engine = FunASRCAMPPEmbeddingEngine(config.campp_model)
                self.enabled = True
                print(f"CAM++ speaker engine enabled via model: {config.campp_model}", flush=True)
            except Exception as exc:  # noqa: BLE001
                print(f"CAM++ speaker engine unavailable, fallback to {DEFAULT_SPEAKER}: {exc}", flush=True)
            return

        print("CAM++ speaker engine not configured; fallback to speaker_1", flush=True)

    def label_for(self, pcm: bytes, session: Session) -> str:
        if not self.enabled or self._engine is None:
            return DEFAULT_SPEAKER
        if duration_ms_for_pcm(pcm) < self.config.min_speaker_ms:
            return session.last_speaker_label or DEFAULT_SPEAKER

        try:
            decision = self._engine.extract(pcm)
        except Exception as exc:  # noqa: BLE001
            print(f"CAM++ embedding failed, fallback to {DEFAULT_SPEAKER}: {exc}", flush=True)
            return DEFAULT_SPEAKER

        if decision.speaker_label:
            session.last_speaker_label = decision.speaker_label
            return decision.speaker_label
        if decision.embedding:
            label, score, created = assign_speaker_label(
                session.speaker_cluster,
                decision.embedding,
                threshold=self.config.speaker_threshold,
                sticky_threshold=self.config.speaker_sticky_threshold,
                sticky_label=session.last_speaker_label,
            )
            print(
                "CAM++ speaker decision",
                f"label={label}",
                f"score={score:.3f}",
                f"created={created}",
                f"clusters={len(session.speaker_cluster.centroids)}",
                f"duration_ms={duration_ms_for_pcm(pcm)}",
                flush=True,
            )
            session.last_speaker_label = label
            return label
        return session.last_speaker_label or DEFAULT_SPEAKER


@dataclass
class CAMPPDecision:
    embedding: list[float] | None = None
    speaker_label: str = ""


class CAMPPEmbeddingEngine:
    def extract(self, pcm: bytes) -> CAMPPDecision:
        raise NotImplementedError


class CommandCAMPPEmbeddingEngine(CAMPPEmbeddingEngine):
    def __init__(self, command: str) -> None:
        self.command = command

    def extract(self, pcm: bytes) -> CAMPPDecision:
        wav_path = write_temp_wav(pcm)
        try:
            command = self.command.replace("{wav}", quote_command_path(wav_path))
            completed = subprocess.run(
                command,
                shell=True,
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
                timeout=20,
            )
            payload = json.loads((completed.stdout or "").strip())
            return decision_from_payload(payload)
        finally:
            try:
                Path(wav_path).unlink(missing_ok=True)
            except Exception:
                pass


class FunASRCAMPPEmbeddingEngine(CAMPPEmbeddingEngine):
    def __init__(self, model: str) -> None:
        from funasr import AutoModel  # type: ignore

        started = time.time()
        self._model = AutoModel(model=model, disable_update=True)
        print(f"funasr CAM++ model loaded cost={time.time() - started:.3f}s", flush=True)

    def extract(self, pcm: bytes) -> CAMPPDecision:
        wav_path = write_temp_wav(pcm)
        try:
            result = self._model.generate(input=wav_path)
            return decision_from_payload(result)
        finally:
            try:
                Path(wav_path).unlink(missing_ok=True)
            except Exception:
                pass


class SherpaCAMPPEmbeddingEngine(CAMPPEmbeddingEngine):
    def __init__(self, config: Config) -> None:
        cfg = sherpa_onnx.SpeakerEmbeddingExtractorConfig()
        cfg.model = config.campp_model
        cfg.num_threads = config.num_threads
        cfg.provider = config.provider
        if not cfg.validate():
            raise ValueError(f"invalid sherpa CAM++ config: {config.campp_model}")
        started = time.time()
        self._extractor = sherpa_onnx.SpeakerEmbeddingExtractor(cfg)
        print(
            "sherpa CAM++ extractor loaded",
            f"model={config.campp_model}",
            f"dim={self._extractor.dim}",
            f"cost={time.time() - started:.3f}s",
            flush=True,
        )

    def extract(self, pcm: bytes) -> CAMPPDecision:
        samples = pcm16_to_float32(pcm)
        stream = self._extractor.create_stream()
        stream.accept_waveform(SAMPLE_RATE, samples)
        stream.input_finished()
        if not self._extractor.is_ready(stream):
            return CAMPPDecision()
        embedding = list(self._extractor.compute(stream))
        return CAMPPDecision(embedding=embedding)


class LocalASRApp:
    def __init__(self, config: Config) -> None:
        self.config = config
        self.engine = SenseVoiceEngine(config)
        self.speaker_diarizer = SpeakerDiarizer(config)
        self.sessions: dict[str, Session] = {}
        self.sessions_lock = threading.Lock()

    def start_session(self, meeting_id: str) -> dict[str, Any]:
        with self.sessions_lock:
            self.sessions[meeting_id] = Session()
        return {"ok": True, "message": "session started"}

    def audio(self, meeting_id: str, body: bytes) -> dict[str, Any]:
        session = self._session(meeting_id)
        events = self._process_audio(meeting_id, session, body, force_flush=False)
        return {"ok": True, "events": events}

    def flush(self, meeting_id: str) -> dict[str, Any]:
        session = self._session(meeting_id)
        events = self._process_audio(meeting_id, session, b"", force_flush=True)
        return {"ok": True, "events": events}

    def stop_session(self, meeting_id: str) -> dict[str, Any]:
        session = self._session(meeting_id)
        events = self._process_audio(meeting_id, session, b"", force_flush=True)
        with self.sessions_lock:
            self.sessions.pop(meeting_id, None)
        return {"ok": True, "events": events}

    def _session(self, meeting_id: str) -> Session:
        with self.sessions_lock:
            session = self.sessions.get(meeting_id)
            if session is None:
                session = Session()
                self.sessions[meeting_id] = session
            return session

    def _process_audio(self, meeting_id: str, session: Session, body: bytes, force_flush: bool) -> list[dict[str, Any]]:
        events: list[dict[str, Any]] = []
        session.pending.extend(body)
        frame_bytes = SAMPLE_RATE * FRAME_MS // 1000 * BYTES_PER_SAMPLE

        offset = 0
        while offset + frame_bytes <= len(session.pending):
            frame = bytes(session.pending[offset : offset + frame_bytes])
            offset += frame_bytes

            energy = average_abs_energy(frame)
            peak = pcm16_peak(frame)
            speech_threshold = max(self.config.vad_threshold, session.noise_floor * self.config.vad_noise_ratio)
            is_speech = energy >= speech_threshold or peak >= self.config.vad_peak_threshold
            if is_speech:
                if not session.in_speech:
                    session.in_speech = True
                    session.segment_start_ms = max(0, session.cursor_ms - duration_ms_for_pcm(session.pre_speech))
                    session.segment.clear()
                    session.segment.extend(session.pre_speech)
                    session.pre_speech.clear()
                session.trailing_silence_ms = 0
                session.segment.extend(frame)
            elif session.in_speech:
                session.trailing_silence_ms += FRAME_MS
                session.segment.extend(frame)
            else:
                session.noise_floor = update_noise_floor(session.noise_floor, energy)
                append_pre_speech(session, frame, self.config.pre_speech_ms)

            session.cursor_ms += FRAME_MS

            if session.in_speech and segment_duration_ms(session) >= self.config.max_segment_ms:
                event = self._final_event(meeting_id, session)
                if event:
                    events.append(event)
                reset_segment(session)
                continue

            if session.in_speech and session.trailing_silence_ms >= self.config.end_silence_ms:
                if segment_duration_ms(session) >= self.config.min_speech_ms:
                    event = self._final_event(meeting_id, session)
                    if event:
                        events.append(event)
                reset_segment(session)

        if offset:
            del session.pending[:offset]

        if force_flush and session.in_speech:
            event = self._final_event(meeting_id, session)
            if event:
                events.append(event)
            reset_segment(session)
        return events

    def _final_event(self, meeting_id: str, session: Session) -> dict[str, Any] | None:
        segment_pcm = bytes(session.segment)
        raw_peak = pcm16_peak(segment_pcm)
        model_pcm, gain = normalize_pcm16(segment_pcm, self.config.normalize_peak, self.config.max_gain)
        text, confidence = self.engine.transcribe(model_pcm)
        if not text:
            print(
                "sensevoice empty segment",
                f"duration_ms={duration_ms_for_pcm(segment_pcm)}",
                f"raw_peak={raw_peak}",
                f"gain={gain:.2f}",
                flush=True,
            )
            return None
        speaker_label = self.speaker_diarizer.label_for(model_pcm, session)
        return {
            "type": "transcript",
            "contractVersion": CONTRACT_VERSION,
            "meetingId": meeting_id,
            "speakerLabel": speaker_label,
            "text": text,
            "isFinal": True,
            "confidence": confidence,
            "provider": PROVIDER,
            "startedAtMs": session.segment_start_ms,
            "endedAtMs": session.segment_start_ms + segment_duration_ms(session),
        }


class RequestHandler(BaseHTTPRequestHandler):
    app: LocalASRApp

    def log_message(self, fmt: str, *args: Any) -> None:
        print("%s - %s" % (self.address_string(), fmt % args), flush=True)

    def do_GET(self) -> None:
        if self.path == "/health":
            self._json({
                "ok": True,
                "provider": PROVIDER,
                "contractVersion": CONTRACT_VERSION,
                "speakerDiarization": self.app.speaker_diarizer.enabled,
            })
            return
        self._json({"ok": False, "error": "not found"}, status=404)

    def do_POST(self) -> None:
        try:
            parts = [unquote(p) for p in urlparse(self.path).path.split("/") if p]
            if len(parts) != 4 or parts[0] != "v1" or parts[1] != "sessions":
                self._json({"ok": False, "error": "not found"}, status=404)
                return
            meeting_id = parts[2]
            action = parts[3]
            content_length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(content_length) if content_length else b""

            if action == "start":
                self._json(self.app.start_session(meeting_id))
            elif action == "audio":
                self._json(self.app.audio(meeting_id, body))
            elif action == "flush":
                self._json(self.app.flush(meeting_id))
            elif action == "stop":
                self._json(self.app.stop_session(meeting_id))
            else:
                self._json({"ok": False, "error": "not found"}, status=404)
        except Exception as exc:  # noqa: BLE001
            self._json({"ok": False, "error": str(exc)}, status=500)

    def _json(self, payload: dict[str, Any], status: int = 200) -> None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def average_abs_energy(frame: bytes) -> float:
    if len(frame) < 2:
        return 0.0
    samples = array.array("h")
    samples.frombytes(frame[: len(frame) - (len(frame) % 2)])
    if not samples:
        return 0.0
    return sum(abs(x) for x in samples) / len(samples)


def pcm16_peak(pcm: bytes) -> int:
    samples = array.array("h")
    samples.frombytes(pcm[: len(pcm) - (len(pcm) % 2)])
    if not samples:
        return 0
    return max(abs(x) for x in samples)


def normalize_pcm16(pcm: bytes, target_peak: int, max_gain: float) -> tuple[bytes, float]:
    if target_peak <= 0 or max_gain <= 1.0:
        return pcm, 1.0
    samples = array.array("h")
    samples.frombytes(pcm[: len(pcm) - (len(pcm) % 2)])
    if not samples:
        return pcm, 1.0
    peak = max(abs(x) for x in samples)
    if peak <= 0 or peak >= target_peak:
        return pcm, 1.0
    gain = min(float(max_gain), float(target_peak) / float(peak))
    if gain <= 1.01:
        return pcm, 1.0
    normalized = array.array("h")
    for sample in samples:
        value = int(round(sample * gain))
        if value > 32767:
            value = 32767
        elif value < -32768:
            value = -32768
        normalized.append(value)
    return normalized.tobytes(), gain


def pcm16_to_float32(pcm: bytes) -> list[float]:
    samples = array.array("h")
    samples.frombytes(pcm[: len(pcm) - (len(pcm) % 2)])
    return [x / 32768.0 for x in samples]


def duration_ms_for_pcm(pcm: bytes) -> int:
    return len(pcm) // (SAMPLE_RATE * BYTES_PER_SAMPLE // 1000)


def write_temp_wav(pcm: bytes) -> str:
    fd, path = tempfile.mkstemp(prefix="meeting-campp-", suffix=".wav")
    os.close(fd)
    with wave.open(path, "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(BYTES_PER_SAMPLE)
        wav.setframerate(SAMPLE_RATE)
        wav.writeframes(pcm)
    return path


def quote_command_path(path: str) -> str:
    escaped = path.replace('"', '\\"')
    return f'"{escaped}"'


def decision_from_payload(payload: Any) -> CAMPPDecision:
    speaker_label = find_string_key(payload, {"speakerLabel", "speaker_label", "spk", "speaker"})
    embedding = find_embedding(payload)
    return CAMPPDecision(embedding=embedding, speaker_label=speaker_label)


def find_string_key(payload: Any, keys: set[str]) -> str:
    if isinstance(payload, dict):
        for key, value in payload.items():
            if key in keys and isinstance(value, str):
                return value.strip()
        for value in payload.values():
            found = find_string_key(value, keys)
            if found:
                return found
    if isinstance(payload, list):
        for item in payload:
            found = find_string_key(item, keys)
            if found:
                return found
    return ""


def find_embedding(payload: Any) -> list[float] | None:
    if is_number_list(payload):
        return normalize_embedding([float(x) for x in payload])
    if isinstance(payload, dict):
        preferred_keys = (
            "spk_embedding",
            "speaker_embedding",
            "embedding",
            "embeddings",
            "xvector",
            "vector",
        )
        for key in preferred_keys:
            if key in payload:
                found = find_embedding(payload[key])
                if found:
                    return found
        for value in payload.values():
            found = find_embedding(value)
            if found:
                return found
    if isinstance(payload, list):
        for item in payload:
            found = find_embedding(item)
            if found:
                return found
    return None


def is_number_list(value: Any) -> bool:
    if not isinstance(value, list) or len(value) < 8:
        return False
    return all(isinstance(item, (int, float)) for item in value)


def normalize_embedding(embedding: list[float]) -> list[float] | None:
    norm = math.sqrt(sum(value * value for value in embedding))
    if norm <= 1e-9:
        return None
    return [value / norm for value in embedding]


def restore_punctuation(text: str, enabled: bool) -> str:
    text = (text or "").strip()
    if not enabled or not text:
        return text
    if contains_sentence_punctuation(text):
        return text
    if text.endswith((",", "，", "、", ";", "；", ":", "：")):
        return text[:-1].rstrip() + "。"
    if looks_like_question(text):
        return text + "？"
    return text + "。"


def contains_sentence_punctuation(text: str) -> bool:
    return any(mark in text for mark in ("。", "？", "?", "！", "!", ".", "；", ";"))


def looks_like_question(text: str) -> bool:
    lowered = text.lower()
    question_terms = (
        "吗",
        "呢",
        "是不是",
        "是否",
        "有没有",
        "为什么",
        "怎么",
        "如何",
        "what",
        "why",
        "how",
        "where",
        "when",
        "who",
    )
    return any(term in lowered for term in question_terms)


def cosine_similarity(left: list[float], right: list[float]) -> float:
    size = min(len(left), len(right))
    if size == 0:
        return 0.0
    return sum(left[index] * right[index] for index in range(size))


def speaker_index_from_label(label: str) -> int:
    normalized = (label or "").strip().lower().replace("-", "_")
    if not normalized.startswith("speaker_"):
        return -1
    try:
        value = int(normalized.split("_", 1)[1])
    except ValueError:
        return -1
    return value - 1


def update_speaker_centroid(cluster: SpeakerCluster, index: int, normalized: list[float]) -> None:
    count = cluster.counts[index]
    updated = [
        (cluster.centroids[index][i] * count + normalized[i]) / (count + 1)
        for i in range(min(len(cluster.centroids[index]), len(normalized)))
    ]
    cluster.centroids[index] = normalize_embedding(updated) or cluster.centroids[index]
    cluster.counts[index] = count + 1


def assign_speaker_label(
    cluster: SpeakerCluster,
    embedding: list[float],
    threshold: float,
    sticky_threshold: float,
    sticky_label: str = "",
) -> tuple[str, float, bool]:
    normalized = normalize_embedding(embedding)
    if not normalized:
        return DEFAULT_SPEAKER, 0.0, False

    best_index = -1
    best_score = -1.0
    for index, centroid in enumerate(cluster.centroids):
        score = cosine_similarity(normalized, centroid)
        if score > best_score:
            best_score = score
            best_index = index

    if best_index >= 0 and best_score >= threshold:
        update_speaker_centroid(cluster, best_index, normalized)
        return f"speaker_{best_index + 1}", best_score, False

    sticky_index = speaker_index_from_label(sticky_label)
    if 0 <= sticky_index < len(cluster.centroids):
        sticky_score = cosine_similarity(normalized, cluster.centroids[sticky_index])
        if sticky_score >= sticky_threshold:
            update_speaker_centroid(cluster, sticky_index, normalized)
            return f"speaker_{sticky_index + 1}", sticky_score, False

    if best_index >= 0 and best_score >= sticky_threshold:
        update_speaker_centroid(cluster, best_index, normalized)
        return f"speaker_{best_index + 1}", best_score, False

    cluster.centroids.append(normalized)
    cluster.counts.append(1)
    return f"speaker_{len(cluster.centroids)}", best_score, True


def segment_duration_ms(session: Session) -> int:
    return len(session.segment) // (SAMPLE_RATE * BYTES_PER_SAMPLE // 1000)


def append_pre_speech(session: Session, frame: bytes, pre_speech_ms: int) -> None:
    if pre_speech_ms <= 0:
        session.pre_speech.clear()
        return
    session.pre_speech.extend(frame)
    max_bytes = SAMPLE_RATE * BYTES_PER_SAMPLE * pre_speech_ms // 1000
    if len(session.pre_speech) > max_bytes:
        del session.pre_speech[: len(session.pre_speech) - max_bytes]


def update_noise_floor(current: float, energy: float) -> float:
    if energy <= 0:
        energy = 1.0
    if current <= 0:
        return energy
    if energy < current:
        return current * 0.90 + energy * 0.10
    return current * 0.985 + energy * 0.015


def reset_segment(session: Session) -> None:
    session.segment.clear()
    session.in_speech = False
    session.trailing_silence_ms = 0
    session.segment_start_ms = session.cursor_ms


def env_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() not in {"0", "false", "no", "off"}


def default_model_dir() -> Path:
    return Path(__file__).resolve().parent / "models" / "sensevoice-windows"


def load_config() -> Config:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.getenv("LOCAL_ASR_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.getenv("LOCAL_ASR_PORT", "9001")))
    parser.add_argument("--model", default=os.getenv("SENSEVOICE_MODEL", ""))
    parser.add_argument("--tokens", default=os.getenv("SENSEVOICE_TOKENS", ""))
    parser.add_argument("--language", default=os.getenv("SENSEVOICE_LANGUAGE", "zh"))
    parser.add_argument("--num-threads", type=int, default=int(os.getenv("SENSEVOICE_NUM_THREADS", "4")))
    parser.add_argument("--provider", default=os.getenv("SENSEVOICE_PROVIDER", "cpu"))
    parser.add_argument("--use-itn", action=argparse.BooleanOptionalAction, default=env_bool("SENSEVOICE_USE_ITN", True))
    parser.add_argument(
        "--enable-punctuation",
        action=argparse.BooleanOptionalAction,
        default=env_bool("SENSEVOICE_ENABLE_PUNCTUATION", True),
    )
    parser.add_argument("--vad-threshold", type=int, default=int(os.getenv("LOCAL_ASR_VAD_THRESHOLD", "120")))
    parser.add_argument("--vad-peak-threshold", type=int, default=int(os.getenv("LOCAL_ASR_VAD_PEAK_THRESHOLD", "900")))
    parser.add_argument("--vad-noise-ratio", type=float, default=float(os.getenv("LOCAL_ASR_VAD_NOISE_RATIO", "3.0")))
    parser.add_argument("--end-silence-ms", type=int, default=int(os.getenv("LOCAL_ASR_END_SILENCE_MS", "550")))
    parser.add_argument("--min-speech-ms", type=int, default=int(os.getenv("LOCAL_ASR_MIN_SPEECH_MS", "500")))
    parser.add_argument("--max-segment-ms", type=int, default=int(os.getenv("LOCAL_ASR_MAX_SEGMENT_MS", "8000")))
    parser.add_argument("--pre-speech-ms", type=int, default=int(os.getenv("LOCAL_ASR_PRE_SPEECH_MS", "300")))
    parser.add_argument("--normalize-peak", type=int, default=int(os.getenv("LOCAL_ASR_NORMALIZE_PEAK", "8000")))
    parser.add_argument("--max-gain", type=float, default=float(os.getenv("LOCAL_ASR_MAX_GAIN", "4")))
    parser.add_argument(
        "--enable-speaker-diarization",
        action=argparse.BooleanOptionalAction,
        default=env_bool("LOCAL_ASR_ENABLE_SPEAKER_DIARIZATION", True),
    )
    parser.add_argument("--campp-model", default=os.getenv("CAMPP_MODEL", os.getenv("CAMPP_MODEL_ID", "")))
    parser.add_argument("--campp-command", default=os.getenv("CAMPP_COMMAND", ""))
    parser.add_argument("--speaker-threshold", type=float, default=float(os.getenv("CAMPP_SPEAKER_THRESHOLD", "0.40")))
    parser.add_argument(
        "--speaker-sticky-threshold",
        type=float,
        default=float(os.getenv("CAMPP_SPEAKER_STICKY_THRESHOLD", "0.28")),
    )
    parser.add_argument("--min-speaker-ms", type=int, default=int(os.getenv("CAMPP_MIN_SPEAKER_MS", "1600")))
    args = parser.parse_args()

    model_dir = default_model_dir()
    model = args.model or str(model_dir / "model.int8.onnx")
    tokens = args.tokens or str(model_dir / "tokens.txt")
    if not Path(model).exists():
        raise FileNotFoundError(f"SenseVoice model not found: {model}")
    if not Path(tokens).exists():
        raise FileNotFoundError(f"SenseVoice tokens not found: {tokens}")
    return Config(
        host=args.host,
        port=args.port,
        model=model,
        tokens=tokens,
        language=args.language,
        num_threads=args.num_threads,
        provider=args.provider,
        use_itn=args.use_itn,
        enable_punctuation=args.enable_punctuation,
        vad_threshold=args.vad_threshold,
        vad_peak_threshold=args.vad_peak_threshold,
        vad_noise_ratio=args.vad_noise_ratio,
        end_silence_ms=args.end_silence_ms,
        min_speech_ms=args.min_speech_ms,
        max_segment_ms=args.max_segment_ms,
        pre_speech_ms=args.pre_speech_ms,
        normalize_peak=args.normalize_peak,
        max_gain=args.max_gain,
        enable_speaker_diarization=args.enable_speaker_diarization,
        campp_model=args.campp_model,
        campp_command=args.campp_command,
        speaker_threshold=args.speaker_threshold,
        speaker_sticky_threshold=args.speaker_sticky_threshold,
        min_speaker_ms=args.min_speaker_ms,
    )


def main() -> None:
    config = load_config()
    RequestHandler.app = LocalASRApp(config)
    server = ThreadingHTTPServer((config.host, config.port), RequestHandler)
    print(f"sensevoice local asr listening on http://{config.host}:{config.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
