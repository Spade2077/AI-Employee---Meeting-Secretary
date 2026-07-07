// 本文件实现 RK3588 本地 ASR 服务的最小版。
// 当前重点是固定服务边界：接收 PCM、做简单 VAD 切段、调用可替换的 SenseVoice 引擎、
// 输出 meeting-asr-v1 转写事件。后续可把命令行引擎替换为 sherpa-onnx C API/RKNN 实现。

#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L
#endif

#include <algorithm>
#include <array>
#include <arpa/inet.h>
#include <cerrno>
#include <chrono>
#include <cctype>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <map>
#include <memory>
#include <netinet/in.h>
#include <sstream>
#include <stdexcept>
#include <string>
#include <sys/socket.h>
#include <sys/types.h>
#include <unistd.h>
#include <utility>
#include <vector>

#ifdef LOCAL_ASR_ENABLE_SHERPA_ONNX
#include "sherpa-onnx/c-api/c-api.h"
#endif

namespace {

constexpr const char* kContractVersion = "meeting-asr-v1";
constexpr const char* kProvider = "sensevoice";
constexpr const char* kDefaultSpeaker = "speaker_1";
constexpr int kSampleRate = 16000;
constexpr int kChannels = 1;
constexpr int kBytesPerSample = 2;
constexpr int kFrameMs = 20;

struct Config {
  int port = 9001;
  int vadThreshold = 500;
  int endSilenceMs = 600;
  int minSpeechMs = 300;
  int senseVoiceNumThreads = 4;
  bool senseVoiceUseITN = true;
  bool senseVoiceEnablePunctuation = true;
  bool senseVoiceDebug = false;
  std::string mockText;
  std::string engine;
  std::string senseVoiceModel;
  std::string senseVoiceTokens;
  std::string senseVoiceLanguage = "zh";
  std::string senseVoiceProvider = "cpu";
  std::string senseVoiceCommand;
};

struct HttpRequest {
  std::string method;
  std::string path;
  std::map<std::string, std::string> headers;
  std::vector<uint8_t> body;
};

struct TranscriptEvent {
  std::string meetingId;
  std::string speakerLabel = kDefaultSpeaker;
  std::string text;
  bool isFinal = true;
  double confidence = 0.0;
  int64_t startedAtMs = 0;
  int64_t endedAtMs = 0;
};

std::string trim(const std::string& input) {
  size_t begin = 0;
  while (begin < input.size() && std::isspace(static_cast<unsigned char>(input[begin]))) {
    ++begin;
  }
  size_t end = input.size();
  while (end > begin && std::isspace(static_cast<unsigned char>(input[end - 1]))) {
    --end;
  }
  return input.substr(begin, end - begin);
}

std::string lower(std::string value) {
  std::transform(value.begin(), value.end(), value.begin(), [](unsigned char c) {
    return static_cast<char>(std::tolower(c));
  });
  return value;
}

std::string envOrEmpty(const char* name) {
  const char* value = std::getenv(name);
  return value == nullptr ? "" : std::string(value);
}

int envOrInt(const char* name, int fallback) {
  const std::string value = envOrEmpty(name);
  if (value.empty()) {
    return fallback;
  }
  try {
    return std::stoi(value);
  } catch (...) {
    return fallback;
  }
}

bool envOrBool(const char* name, bool fallback) {
  const std::string value = lower(trim(envOrEmpty(name)));
  if (value.empty()) {
    return fallback;
  }
  return value == "1" || value == "true" || value == "yes" || value == "on";
}

std::string jsonEscape(const std::string& input) {
  std::ostringstream out;
  for (char c : input) {
    switch (c) {
      case '"':
        out << "\\\"";
        break;
      case '\\':
        out << "\\\\";
        break;
      case '\b':
        out << "\\b";
        break;
      case '\f':
        out << "\\f";
        break;
      case '\n':
        out << "\\n";
        break;
      case '\r':
        out << "\\r";
        break;
      case '\t':
        out << "\\t";
        break;
      default:
        if (static_cast<unsigned char>(c) < 0x20) {
          out << "\\u00";
          const char* hex = "0123456789abcdef";
          out << hex[(c >> 4) & 0x0f] << hex[c & 0x0f];
        } else {
          out << c;
        }
    }
  }
  return out.str();
}

std::string eventToJSON(const TranscriptEvent& event) {
  std::ostringstream out;
  out << "{"
      << "\"type\":\"transcript\","
      << "\"contractVersion\":\"" << kContractVersion << "\","
      << "\"meetingId\":\"" << jsonEscape(event.meetingId) << "\","
      << "\"speakerLabel\":\"" << jsonEscape(event.speakerLabel) << "\","
      << "\"text\":\"" << jsonEscape(event.text) << "\","
      << "\"isFinal\":" << (event.isFinal ? "true" : "false") << ","
      << "\"confidence\":" << event.confidence << ","
      << "\"provider\":\"" << kProvider << "\","
      << "\"startedAtMs\":" << event.startedAtMs << ","
      << "\"endedAtMs\":" << event.endedAtMs
      << "}";
  return out.str();
}

std::string eventsResponse(const std::vector<TranscriptEvent>& events) {
  std::ostringstream out;
  out << "{\"ok\":true,\"events\":[";
  for (size_t i = 0; i < events.size(); ++i) {
    if (i > 0) {
      out << ",";
    }
    out << eventToJSON(events[i]);
  }
  out << "]}";
  return out.str();
}

std::string errorResponse(const std::string& message) {
  return "{\"ok\":false,\"error\":\"" + jsonEscape(message) + "\"}";
}

bool sendAll(int fd, const std::string& data) {
  const char* ptr = data.data();
  size_t remaining = data.size();
  while (remaining > 0) {
    ssize_t sent = ::send(fd, ptr, remaining, 0);
    if (sent <= 0) {
      return false;
    }
    ptr += sent;
    remaining -= static_cast<size_t>(sent);
  }
  return true;
}

void sendHTTP(int fd, int status, const std::string& body, const std::string& contentType = "application/json") {
  const char* reason = status == 200 ? "OK" : status == 404 ? "Not Found" : "Bad Request";
  std::ostringstream response;
  response << "HTTP/1.1 " << status << " " << reason << "\r\n"
           << "Content-Type: " << contentType << "; charset=utf-8\r\n"
           << "Content-Length: " << body.size() << "\r\n"
           << "Connection: close\r\n"
           << "\r\n"
           << body;
  sendAll(fd, response.str());
}

bool readHTTP(int fd, HttpRequest& request) {
  std::string raw;
  std::vector<uint8_t> buffer(4096);
  size_t headerEnd = std::string::npos;

  while (headerEnd == std::string::npos) {
    ssize_t received = ::recv(fd, buffer.data(), buffer.size(), 0);
    if (received <= 0) {
      return false;
    }
    raw.append(reinterpret_cast<const char*>(buffer.data()), static_cast<size_t>(received));
    headerEnd = raw.find("\r\n\r\n");
    if (raw.size() > 1024 * 1024) {
      return false;
    }
  }

  std::istringstream headerStream(raw.substr(0, headerEnd));
  std::string requestLine;
  std::getline(headerStream, requestLine);
  requestLine = trim(requestLine);
  std::istringstream requestLineStream(requestLine);
  std::string httpVersion;
  requestLineStream >> request.method >> request.path >> httpVersion;
  if (request.method.empty() || request.path.empty()) {
    return false;
  }

  std::string line;
  while (std::getline(headerStream, line)) {
    line = trim(line);
    if (line.empty()) {
      continue;
    }
    const size_t colon = line.find(':');
    if (colon == std::string::npos) {
      continue;
    }
    request.headers[lower(trim(line.substr(0, colon)))] = trim(line.substr(colon + 1));
  }

  size_t contentLength = 0;
  auto it = request.headers.find("content-length");
  if (it != request.headers.end()) {
    contentLength = static_cast<size_t>(std::stoul(it->second));
  }

  const size_t bodyStart = headerEnd + 4;
  if (raw.size() > bodyStart) {
    request.body.assign(raw.begin() + static_cast<long>(bodyStart), raw.end());
  }
  while (request.body.size() < contentLength) {
    ssize_t received = ::recv(fd, buffer.data(), buffer.size(), 0);
    if (received <= 0) {
      return false;
    }
    request.body.insert(request.body.end(), buffer.begin(), buffer.begin() + received);
  }
  if (request.body.size() > contentLength) {
    request.body.resize(contentLength);
  }
  return true;
}

std::vector<std::string> splitPath(const std::string& path) {
  std::vector<std::string> parts;
  std::string cleanPath = path;
  const size_t query = cleanPath.find('?');
  if (query != std::string::npos) {
    cleanPath = cleanPath.substr(0, query);
  }
  std::istringstream input(cleanPath);
  std::string part;
  while (std::getline(input, part, '/')) {
    if (!part.empty()) {
      parts.push_back(part);
    }
  }
  return parts;
}

std::string replaceAll(std::string text, const std::string& from, const std::string& to) {
  size_t pos = 0;
  while ((pos = text.find(from, pos)) != std::string::npos) {
    text.replace(pos, from.size(), to);
    pos += to.size();
  }
  return text;
}

bool containsAny(const std::string& text, const std::vector<std::string>& values) {
  for (const std::string& value : values) {
    if (!value.empty() && text.find(value) != std::string::npos) {
      return true;
    }
  }
  return false;
}

bool endsWith(const std::string& text, const std::string& suffix) {
  return text.size() >= suffix.size() && text.compare(text.size() - suffix.size(), suffix.size(), suffix) == 0;
}

std::string removeTrailingBreakPunctuation(const std::string& text) {
  static const std::vector<std::string> suffixes = {",", "，", "、", ";", "；", ":", "："};
  for (const std::string& suffix : suffixes) {
    if (endsWith(text, suffix)) {
      return trim(text.substr(0, text.size() - suffix.size()));
    }
  }
  return text;
}

bool containsSentencePunctuation(const std::string& text) {
  static const std::vector<std::string> marks = {"。", "？", "?", "！", "!", ".", "；", ";"};
  return containsAny(text, marks);
}

bool looksLikeQuestion(const std::string& text) {
  static const std::vector<std::string> terms = {
      "吗",   "呢",   "是不是", "是否", "有没有", "为什么",
      "怎么", "如何", "what",   "why",  "how",    "where",
      "when", "who",
  };
  return containsAny(lower(text), terms);
}

std::string restorePunctuation(std::string text, bool enabled) {
  text = trim(text);
  if (!enabled || text.empty()) {
    return text;
  }
  if (containsSentencePunctuation(text)) {
    return text;
  }
  text = removeTrailingBreakPunctuation(text);
  if (looksLikeQuestion(text)) {
    return text + "？";
  }
  return text + "。";
}

void writeLE16(std::ofstream& out, uint16_t value) {
  out.put(static_cast<char>(value & 0xff));
  out.put(static_cast<char>((value >> 8) & 0xff));
}

void writeLE32(std::ofstream& out, uint32_t value) {
  out.put(static_cast<char>(value & 0xff));
  out.put(static_cast<char>((value >> 8) & 0xff));
  out.put(static_cast<char>((value >> 16) & 0xff));
  out.put(static_cast<char>((value >> 24) & 0xff));
}

bool writeWav(const std::string& path, const std::vector<uint8_t>& pcm, int sampleRate) {
  std::ofstream out(path, std::ios::binary);
  if (!out) {
    return false;
  }
  const uint16_t bitsPerSample = 16;
  const uint16_t blockAlign = kChannels * bitsPerSample / 8;
  const uint32_t byteRate = sampleRate * blockAlign;
  const uint32_t dataSize = static_cast<uint32_t>(pcm.size());
  const uint32_t chunkSize = 36 + dataSize;

  out.write("RIFF", 4);
  writeLE32(out, chunkSize);
  out.write("WAVE", 4);
  out.write("fmt ", 4);
  writeLE32(out, 16);
  writeLE16(out, 1);
  writeLE16(out, kChannels);
  writeLE32(out, static_cast<uint32_t>(sampleRate));
  writeLE32(out, byteRate);
  writeLE16(out, blockAlign);
  writeLE16(out, bitsPerSample);
  out.write("data", 4);
  writeLE32(out, dataSize);
  out.write(reinterpret_cast<const char*>(pcm.data()), static_cast<std::streamsize>(pcm.size()));
  return true;
}

std::string runCommand(const std::string& command) {
  std::array<char, 512> buffer{};
  std::string output;
  FILE* pipe = ::popen(command.c_str(), "r");
  if (pipe == nullptr) {
    return "";
  }
  while (fgets(buffer.data(), static_cast<int>(buffer.size()), pipe) != nullptr) {
    output += buffer.data();
  }
  ::pclose(pipe);
  return output;
}

std::string lastNonEmptyLine(const std::string& output) {
  std::istringstream input(output);
  std::string line;
  std::string last;
  while (std::getline(input, line)) {
    line = trim(line);
    if (!line.empty()) {
      last = line;
    }
  }
  return last;
}

class SenseVoiceEngine {
 public:
  virtual ~SenseVoiceEngine() = default;
  virtual std::pair<std::string, double> transcribe(const std::vector<uint8_t>& pcm, int sampleRate) = 0;
};

class EmptySenseVoiceEngine final : public SenseVoiceEngine {
 public:
  std::pair<std::string, double> transcribe(const std::vector<uint8_t>&, int) override {
    return {"", 0.0};
  }
};

class MockSenseVoiceEngine final : public SenseVoiceEngine {
 public:
  explicit MockSenseVoiceEngine(std::string text) : text_(std::move(text)) {}

  std::pair<std::string, double> transcribe(const std::vector<uint8_t>&, int) override {
    return {text_, 0.9};
  }

 private:
  std::string text_;
};

class CommandSenseVoiceEngine final : public SenseVoiceEngine {
 public:
  explicit CommandSenseVoiceEngine(std::string commandTemplate)
      : commandTemplate_(std::move(commandTemplate)) {}

  std::pair<std::string, double> transcribe(const std::vector<uint8_t>& pcm, int sampleRate) override {
    char tempName[] = "/tmp/local-asr-XXXXXX";
    const int fd = ::mkstemp(tempName);
    if (fd < 0) {
      std::cerr << "create temp wav failed: " << std::strerror(errno) << std::endl;
      return {"", 0.0};
    }
    ::close(fd);

    const std::string wavPath(tempName);
    if (!writeWav(wavPath, pcm, sampleRate)) {
      std::remove(wavPath.c_str());
      return {"", 0.0};
    }

    std::string command = replaceAll(commandTemplate_, "{wav}", wavPath);
    const std::string output = runCommand(command);
    std::remove(wavPath.c_str());

    std::string text = lastNonEmptyLine(output);
    return {text, text.empty() ? 0.0 : 0.88};
  }

 private:
  std::string commandTemplate_;
};

#ifdef LOCAL_ASR_ENABLE_SHERPA_ONNX
std::vector<float> pcm16ToFloat32(const std::vector<uint8_t>& pcm) {
  std::vector<float> samples;
  samples.reserve(pcm.size() / 2);
  for (size_t i = 0; i + 1 < pcm.size(); i += 2) {
    const int16_t sample = static_cast<int16_t>(pcm[i] | (pcm[i + 1] << 8));
    samples.push_back(static_cast<float>(sample) / 32768.0f);
  }
  return samples;
}

class SherpaSenseVoiceEngine final : public SenseVoiceEngine {
 public:
  explicit SherpaSenseVoiceEngine(Config config) : config_(std::move(config)) {
    if (config_.senseVoiceModel.empty() || config_.senseVoiceTokens.empty()) {
      throw std::runtime_error("SENSEVOICE_MODEL and SENSEVOICE_TOKENS are required for sherpa engine");
    }

    SherpaOnnxOfflineRecognizerConfig recognizerConfig;
    std::memset(&recognizerConfig, 0, sizeof(recognizerConfig));
    recognizerConfig.model_config.sense_voice.model = config_.senseVoiceModel.c_str();
    recognizerConfig.model_config.sense_voice.language = config_.senseVoiceLanguage.c_str();
    recognizerConfig.model_config.sense_voice.use_itn = config_.senseVoiceUseITN ? 1 : 0;
    recognizerConfig.model_config.tokens = config_.senseVoiceTokens.c_str();
    recognizerConfig.model_config.num_threads = config_.senseVoiceNumThreads;
    recognizerConfig.model_config.debug = config_.senseVoiceDebug ? 1 : 0;
    recognizerConfig.model_config.provider = config_.senseVoiceProvider.c_str();
    recognizerConfig.decoding_method = "greedy_search";
    recognizerConfig.max_active_paths = 4;

    recognizer_ = SherpaOnnxCreateOfflineRecognizer(&recognizerConfig);
    if (recognizer_ == nullptr) {
      throw std::runtime_error("failed to create sherpa-onnx SenseVoice recognizer");
    }
    std::cout << "using sherpa-onnx SenseVoice engine"
              << " model=" << config_.senseVoiceModel
              << " tokens=" << config_.senseVoiceTokens
              << " provider=" << config_.senseVoiceProvider
              << " threads=" << config_.senseVoiceNumThreads
              << std::endl;
  }

  ~SherpaSenseVoiceEngine() override {
    if (recognizer_ != nullptr) {
      SherpaOnnxDestroyOfflineRecognizer(recognizer_);
      recognizer_ = nullptr;
    }
  }

  SherpaSenseVoiceEngine(const SherpaSenseVoiceEngine&) = delete;
  SherpaSenseVoiceEngine& operator=(const SherpaSenseVoiceEngine&) = delete;

  std::pair<std::string, double> transcribe(const std::vector<uint8_t>& pcm, int sampleRate) override {
    if (recognizer_ == nullptr || pcm.empty()) {
      return {"", 0.0};
    }

    const std::vector<float> samples = pcm16ToFloat32(pcm);
    SherpaOnnxOfflineStream* stream = SherpaOnnxCreateOfflineStream(recognizer_);
    if (stream == nullptr) {
      return {"", 0.0};
    }

    SherpaOnnxAcceptWaveformOffline(stream, sampleRate, samples.data(), static_cast<int32_t>(samples.size()));
    SherpaOnnxDecodeOfflineStream(recognizer_, stream);
    const SherpaOnnxOfflineRecognizerResult* result = SherpaOnnxGetOfflineStreamResult(stream);

    std::string text;
    if (result != nullptr && result->text != nullptr) {
      text = result->text;
    }
    if (result != nullptr) {
      SherpaOnnxDestroyOfflineRecognizerResult(result);
    }
    SherpaOnnxDestroyOfflineStream(stream);
    return {trim(text), text.empty() ? 0.0 : 0.9};
  }

 private:
  Config config_;
  const SherpaOnnxOfflineRecognizer* recognizer_ = nullptr;
};
#endif

struct Session {
  std::vector<uint8_t> pending;
  std::vector<uint8_t> segment;
  bool inSpeech = false;
  int trailingSilenceMs = 0;
  int64_t cursorMs = 0;
  int64_t segmentStartMs = 0;
};

double averageAbsEnergy(const std::vector<uint8_t>& frame) {
  if (frame.size() < 2) {
    return 0.0;
  }
  int64_t sum = 0;
  size_t samples = 0;
  for (size_t i = 0; i + 1 < frame.size(); i += 2) {
    int16_t sample = static_cast<int16_t>(frame[i] | (frame[i + 1] << 8));
    sum += std::abs(static_cast<int>(sample));
    ++samples;
  }
  return samples == 0 ? 0.0 : static_cast<double>(sum) / static_cast<double>(samples);
}

class LocalASRService {
 public:
  LocalASRService(Config config, std::unique_ptr<SenseVoiceEngine> engine)
      : config_(std::move(config)), engine_(std::move(engine)) {}

  void serve() {
    const int serverFd = ::socket(AF_INET, SOCK_STREAM, 0);
    if (serverFd < 0) {
      throw std::runtime_error("socket failed");
    }

    int opt = 1;
    ::setsockopt(serverFd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    sockaddr_in address{};
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = INADDR_ANY;
    address.sin_port = htons(static_cast<uint16_t>(config_.port));

    if (::bind(serverFd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) < 0) {
      ::close(serverFd);
      throw std::runtime_error("bind failed");
    }
    if (::listen(serverFd, 16) < 0) {
      ::close(serverFd);
      throw std::runtime_error("listen failed");
    }

    std::cout << "local-asr-service listening on http://0.0.0.0:" << config_.port << std::endl;

    while (true) {
      sockaddr_in clientAddress{};
      socklen_t clientLength = sizeof(clientAddress);
      const int clientFd = ::accept(serverFd, reinterpret_cast<sockaddr*>(&clientAddress), &clientLength);
      if (clientFd < 0) {
        continue;
      }
      handleClient(clientFd);
      ::close(clientFd);
    }
  }

 private:
  void handleClient(int clientFd) {
    HttpRequest request;
    if (!readHTTP(clientFd, request)) {
      sendHTTP(clientFd, 400, errorResponse("invalid http request"));
      return;
    }

    if (request.method == "GET" && request.path == "/health") {
      sendHTTP(clientFd, 200, "{\"ok\":true,\"provider\":\"sensevoice\",\"contractVersion\":\"meeting-asr-v1\"}");
      return;
    }

    const std::vector<std::string> parts = splitPath(request.path);
    if (parts.size() != 4 || parts[0] != "v1" || parts[1] != "sessions") {
      sendHTTP(clientFd, 404, errorResponse("not found"));
      return;
    }

    const std::string& meetingId = parts[2];
    const std::string& action = parts[3];
    if (request.method == "POST" && action == "start") {
      sessions_[meetingId] = Session{};
      sendHTTP(clientFd, 200, "{\"ok\":true,\"message\":\"session started\"}");
      return;
    }
    if (request.method == "POST" && action == "audio") {
      Session& session = sessions_[meetingId];
      std::vector<TranscriptEvent> events = processAudio(meetingId, session, request.body, false);
      sendHTTP(clientFd, 200, eventsResponse(events));
      return;
    }
    if (request.method == "POST" && action == "flush") {
      Session& session = sessions_[meetingId];
      std::vector<TranscriptEvent> events = processAudio(meetingId, session, {}, true);
      sendHTTP(clientFd, 200, eventsResponse(events));
      return;
    }
    if (request.method == "POST" && action == "stop") {
      Session& session = sessions_[meetingId];
      std::vector<TranscriptEvent> events = processAudio(meetingId, session, {}, true);
      sessions_.erase(meetingId);
      sendHTTP(clientFd, 200, eventsResponse(events));
      return;
    }
    if (request.method == "DELETE" && action == "stop") {
      sessions_.erase(meetingId);
      sendHTTP(clientFd, 200, "{\"ok\":true,\"message\":\"session removed\"}");
      return;
    }

    sendHTTP(clientFd, 404, errorResponse("not found"));
  }

  std::vector<TranscriptEvent> processAudio(
      const std::string& meetingId,
      Session& session,
      const std::vector<uint8_t>& body,
      bool forceFlush) {
    std::vector<TranscriptEvent> events;
    session.pending.insert(session.pending.end(), body.begin(), body.end());

    const size_t frameBytes = static_cast<size_t>(kSampleRate * kFrameMs / 1000 * kBytesPerSample);
    size_t offset = 0;
    while (offset + frameBytes <= session.pending.size()) {
      std::vector<uint8_t> frame(
          session.pending.begin() + static_cast<long>(offset),
          session.pending.begin() + static_cast<long>(offset + frameBytes));
      offset += frameBytes;

      const bool isSpeech = averageAbsEnergy(frame) >= static_cast<double>(config_.vadThreshold);
      if (isSpeech) {
        if (!session.inSpeech) {
          session.inSpeech = true;
          session.segmentStartMs = session.cursorMs;
          session.segment.clear();
        }
        session.trailingSilenceMs = 0;
        session.segment.insert(session.segment.end(), frame.begin(), frame.end());
      } else if (session.inSpeech) {
        session.trailingSilenceMs += kFrameMs;
        session.segment.insert(session.segment.end(), frame.begin(), frame.end());
      }

      session.cursorMs += kFrameMs;

      if (session.inSpeech && session.trailingSilenceMs >= config_.endSilenceMs) {
        if (segmentDurationMs(session) >= config_.minSpeechMs) {
          appendFinalEvent(meetingId, session, events);
        }
        resetSegment(session);
      }
    }

    if (offset > 0) {
      session.pending.erase(session.pending.begin(), session.pending.begin() + static_cast<long>(offset));
    }

    if (forceFlush && session.inSpeech) {
      appendFinalEvent(meetingId, session, events);
      resetSegment(session);
    }
    return events;
  }

  int64_t segmentDurationMs(const Session& session) const {
    const int64_t bytesPerMs = kSampleRate * kBytesPerSample / 1000;
    if (bytesPerMs <= 0) {
      return 0;
    }
    return static_cast<int64_t>(session.segment.size()) / bytesPerMs;
  }

  void appendFinalEvent(const std::string& meetingId, Session& session, std::vector<TranscriptEvent>& events) {
    auto [text, confidence] = engine_->transcribe(session.segment, kSampleRate);
    text = restorePunctuation(text, config_.senseVoiceEnablePunctuation);
    if (text.empty()) {
      return;
    }
    TranscriptEvent event;
    event.meetingId = meetingId;
    event.text = text;
    event.confidence = confidence;
    event.startedAtMs = session.segmentStartMs;
    event.endedAtMs = session.segmentStartMs + segmentDurationMs(session);
    events.push_back(event);
  }

  void resetSegment(Session& session) {
    session.segment.clear();
    session.inSpeech = false;
    session.trailingSilenceMs = 0;
    session.segmentStartMs = session.cursorMs;
  }

  Config config_;
  std::unique_ptr<SenseVoiceEngine> engine_;
  std::map<std::string, Session> sessions_;
};

Config loadConfig(int argc, char** argv) {
  Config config;
  config.vadThreshold = envOrInt("LOCAL_ASR_VAD_THRESHOLD", config.vadThreshold);
  config.endSilenceMs = envOrInt("LOCAL_ASR_END_SILENCE_MS", config.endSilenceMs);
  config.minSpeechMs = envOrInt("LOCAL_ASR_MIN_SPEECH_MS", config.minSpeechMs);
  config.mockText = envOrEmpty("LOCAL_ASR_MOCK_TEXT");
  config.engine = lower(trim(envOrEmpty("LOCAL_ASR_ENGINE")));
  config.senseVoiceModel = envOrEmpty("SENSEVOICE_MODEL");
  config.senseVoiceTokens = envOrEmpty("SENSEVOICE_TOKENS");
  config.senseVoiceLanguage = envOrEmpty("SENSEVOICE_LANGUAGE");
  if (config.senseVoiceLanguage.empty()) {
    config.senseVoiceLanguage = "zh";
  }
  config.senseVoiceProvider = envOrEmpty("SENSEVOICE_PROVIDER");
  if (config.senseVoiceProvider.empty()) {
    config.senseVoiceProvider = "cpu";
  }
  config.senseVoiceNumThreads = envOrInt("SENSEVOICE_NUM_THREADS", config.senseVoiceNumThreads);
  config.senseVoiceUseITN = envOrBool("SENSEVOICE_USE_ITN", config.senseVoiceUseITN);
  config.senseVoiceEnablePunctuation =
      envOrBool("SENSEVOICE_ENABLE_PUNCTUATION", config.senseVoiceEnablePunctuation);
  config.senseVoiceDebug = envOrBool("SENSEVOICE_DEBUG", config.senseVoiceDebug);
  config.senseVoiceCommand = envOrEmpty("SENSEVOICE_COMMAND");

  for (int i = 1; i < argc; ++i) {
    const std::string arg = argv[i];
    if (arg == "--port" && i + 1 < argc) {
      config.port = std::stoi(argv[++i]);
    } else if (arg == "--vad-threshold" && i + 1 < argc) {
      config.vadThreshold = std::stoi(argv[++i]);
    } else if (arg == "--end-silence-ms" && i + 1 < argc) {
      config.endSilenceMs = std::stoi(argv[++i]);
    } else if (arg == "--engine" && i + 1 < argc) {
      config.engine = lower(trim(argv[++i]));
    } else if (arg == "--sensevoice-model" && i + 1 < argc) {
      config.senseVoiceModel = argv[++i];
    } else if (arg == "--sensevoice-tokens" && i + 1 < argc) {
      config.senseVoiceTokens = argv[++i];
    } else if (arg == "--enable-punctuation") {
      config.senseVoiceEnablePunctuation = true;
    } else if (arg == "--disable-punctuation") {
      config.senseVoiceEnablePunctuation = false;
    }
  }
  return config;
}

std::unique_ptr<SenseVoiceEngine> createEngine(const Config& config) {
  const bool wantsSherpa = config.engine == "sherpa" || config.engine == "sensevoice" ||
                           (!config.senseVoiceModel.empty() && !config.senseVoiceTokens.empty());
  if (wantsSherpa) {
#ifdef LOCAL_ASR_ENABLE_SHERPA_ONNX
    return std::make_unique<SherpaSenseVoiceEngine>(config);
#else
    throw std::runtime_error(
        "sherpa SenseVoice engine requested, but binary was built without LOCAL_ASR_USE_SHERPA_ONNX=ON");
#endif
  }
  if (!config.senseVoiceCommand.empty()) {
    std::cout << "using SenseVoice command engine" << std::endl;
    return std::make_unique<CommandSenseVoiceEngine>(config.senseVoiceCommand);
  }
  if (!config.mockText.empty()) {
    std::cout << "using mock ASR engine" << std::endl;
    return std::make_unique<MockSenseVoiceEngine>(config.mockText);
  }
  std::cout << "no SenseVoice command configured; service will return no transcripts" << std::endl;
  return std::make_unique<EmptySenseVoiceEngine>();
}

}  // namespace

int main(int argc, char** argv) {
  try {
    Config config = loadConfig(argc, argv);
    auto engine = createEngine(config);
    LocalASRService service(std::move(config), std::move(engine));
    service.serve();
  } catch (const std::exception& err) {
    std::cerr << "local-asr-service failed: " << err.what() << std::endl;
    return 1;
  }
  return 0;
}
