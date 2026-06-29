function defaultAPIBase() {
  const configured = import.meta.env.VITE_API_BASE_URL || "";
  if (configured) return configured;
  if (window.location.port === "5173" || window.location.port === "5174") {
    return `${window.location.protocol}//${window.location.hostname}:8090`;
  }
  return "";
}

const API_BASE = defaultAPIBase().replace(/\/$/, "");

export function apiPath(path) {
  return `${API_BASE}${path}`;
}

export function wsPath(path) {
  if (API_BASE) {
    return `${API_BASE.replace(/^http/, "ws")}${path}`;
  }
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
}

export async function fetchJSON(path, options = {}) {
  const response = await fetch(apiPath(path), options);
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error((body && body.error) || `HTTP ${response.status}`);
  }
  return body;
}
