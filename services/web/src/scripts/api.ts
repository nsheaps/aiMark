// Shared client-side helpers for talking to the aiMark API.
//
// In production the API lives on the SAME origin as this site (one Cloudflare
// Worker serves /v1/* and the static assets), so all fetches use relative
// URLs. For local dev / screenshots, the Layout emits
//   <meta name="aimark-api" content="...">
// from PUBLIC_AIMARK_API, and we prefix requests with it.

export function apiBase(): string {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="aimark-api"]');
  return meta?.content?.replace(/\/+$/, "") ?? "";
}

/**
 * Fetch a /v1/... endpoint. Returns the parsed JSON, or null on any failure
 * (network error, non-2xx, bad JSON) — callers render a friendly empty state
 * instead of a broken page.
 */
export async function apiGet<T>(path: string): Promise<T | null> {
  try {
    const res = await fetch(apiBase() + path, { headers: { accept: "application/json" } });
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------- API shapes

export interface LeaderboardRow {
  run_id: string;
  model: string;
  runtime?: string;
  provider?: string;
  quantization?: string;
  hardware_profile_id?: string;
  scores: Record<string, number | undefined>;
  composite: number;
  created_at: string;
  source?: string;
  status?: string;
}

export interface RunDetail {
  run_id?: string;
  id?: string;
  cli?: { version?: string };
  suite?: { id?: string; version?: number };
  target?: Record<string, unknown>;
  environment?: { hardware_profile?: Record<string, unknown> };
  metrics?: Record<string, number>;
  scores?: Record<string, number | undefined> & { composite?: number };
  composite?: number;
  status?: string;
  source?: string;
  created_at?: string;
}

// ---------------------------------------------------------------- formatting

export function fmtScore(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return "—";
  return Math.round(value).toLocaleString("en-US");
}

export function fmtDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toISOString().slice(0, 10);
}

/** Units inferred from aiMark metric key conventions (ttft_ms_p50, decode_tps_mean, ...). */
export function metricUnit(key: string): string {
  if (/_cv\b|_cv$/.test(key)) return "CV";
  if (/_ms(_|$)/.test(key)) return "ms";
  if (/_tps(_|$)/.test(key) || /tok_s|tokens_per_second/.test(key)) return "tok/s";
  if (/_usd|cost/.test(key)) return "USD";
  if (/_pct|accuracy|rate/.test(key)) return "%";
  return "";
}

/** Whether lower values are better for a metric key (latencies, variance, cost). */
export function lowerIsBetter(key: string): boolean {
  return /ttft|latency|_ms(_|$)|_cv(_|$)|_cv$|cost|itl/.test(key);
}

export function fmtMetric(key: string, value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return "—";
  const unit = metricUnit(key);
  let num: string;
  if (Math.abs(value) >= 100) num = Math.round(value).toLocaleString("en-US");
  else if (Math.abs(value) >= 1) num = value.toFixed(1);
  else num = value.toFixed(3);
  return unit && unit !== "CV" ? `${num} ${unit}` : num;
}

/**
 * Color grade for a score cell relative to the 1000-point reference anchor.
 * Returns a CSS class defined in global.css.
 */
export function scoreGrade(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return "";
  if (value >= 1500) return "grade-great";
  if (value >= 1000) return "grade-good";
  if (value >= 500) return "grade-mid";
  return "grade-low";
}

// ---------------------------------------------------------------- DOM helpers

type Child = Node | string | null | undefined;

/** Tiny element builder — text goes through textContent, never innerHTML. */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else node.setAttribute(k, v);
  }
  for (const child of children) {
    if (child == null) continue;
    node.append(child);
  }
  return node;
}

export function statusBadge(status: string | undefined): HTMLElement {
  const s = (status ?? "accepted").toLowerCase();
  const cls =
    s === "accepted" ? "badge badge--ok" : s === "flagged" ? "badge badge--warn" : "badge";
  return el("span", { class: cls }, s);
}

export function sourceBadge(source: string | undefined): HTMLElement | null {
  const s = (source ?? "").toLowerCase();
  if (s === "ci")
    return el(
      "span",
      { class: "badge badge--info", title: "Submitted by aiMark CI infrastructure" },
      "ci run",
    );
  if (s === "dev")
    return el("span", { class: "badge", title: "Submitted by a development build" }, "dev run");
  return null;
}

/** Render a friendly status message into a container (loading / empty / error). */
export function setDataStatus(container: HTMLElement, headline: string, detail?: string): void {
  container.replaceChildren(
    el(
      "div",
      { class: "data-status", role: "status" },
      el("p", {}, el("strong", {}, headline)),
      detail ? el("p", {}, detail) : null,
    ),
  );
}
