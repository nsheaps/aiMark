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
  // The detail API flattens target/hardware to the top level; nested forms
  // kept for forward compatibility with raw envelopes.
  model?: string | null;
  runtime?: string | null;
  provider?: string | null;
  quantization?: string | null;
  track?: string | null;
  params?: Record<string, unknown> | null;
  hardware_profile?: Record<string, unknown> | null;
  target?: Record<string, unknown>;
  environment?: { hardware_profile?: Record<string, unknown> };
  metrics?: Record<string, number>;
  scores?: Record<string, number | undefined> & { composite?: number };
  composite?: number;
  status?: string;
  source?: string;
  created_at?: string;
}

/** One aggregate row from /v1/models or /v1/hardware: a dimension value's run
 * count + median composite on one (suite, version, track) board. */
export interface DimensionAggRow {
  suite: string;
  version: number;
  track: string;
  n: number;
  median_composite: number | null;
}

export interface ModelAggRow extends DimensionAggRow {
  model: string;
}

export interface HardwareAggRow extends DimensionAggRow {
  hardware_profile_id: string;
}

export interface SuiteSummary {
  id: string;
  version: number;
  status: string;
  title: string;
  tracks: string[];
  task_count: number;
}

export interface ParamImpactGroup {
  value: string;
  n: number;
  median_composite: number | null;
}

// ------------------------------------------------- zero-choice benchmark shapes

/** The four bench-1 capability classes, in size order. */
export const BENCH_CLASSES = ["compact", "mainstream", "performance", "ultra"] as const;
export type BenchClass = (typeof BENCH_CLASSES)[number];

export function classTitle(cls: string): string {
  return cls.charAt(0).toUpperCase() + cls.slice(1);
}

export interface SystemsHardware {
  cpu_model: string | null;
  gpu: string | null;
  gpu_vram_gb: number | null;
  ram_gb: number | null;
  unified_memory: boolean | null;
}

export interface SystemsRow {
  bench_id: string;
  class: string;
  hardware: SystemsHardware;
  hardware_profile_id?: string | null;
  scores: Record<string, number | undefined>;
  composite: number;
  source?: string;
  status?: string;
  created_at: string;
}

export interface BenchCell {
  cell_id: string;
  run_id: string;
  role: string;
  suite?: { id: string; version: number } | null;
  model?: string | null;
  status?: string | null;
  composite: number | null;
}

export interface BenchDetail {
  bench_id: string;
  program: { id: string; version: number };
  class: string;
  classification?: {
    accel_mem_gb?: number;
    cpu_only?: boolean;
    unified_memory?: boolean;
    detail?: string;
  } | null;
  source?: string;
  status?: string;
  flag_reason?: string | null;
  created_at?: string;
  cli?: { version?: string } | null;
  hardware_profile?: Record<string, unknown> | null;
  scores?: Record<string, number | undefined>;
  composite?: number | null;
  cells?: BenchCell[];
}

export interface ModelFitRow {
  model: string;
  quantization: string | null;
  cohort: string;
  cohort_kind: "gpu" | "cpu";
  n: number;
  decode_tps_median: number;
  ttft_ms_p50_median: number | null;
  usability: "instant" | "usable" | "painful";
}

export interface MonitorPoint {
  date: string;
  n: number;
  decode_tps_median: number | null;
  ttft_ms_p50_median: number | null;
}

export interface MonitorSeries {
  provider: string;
  model: string;
  points: MonitorPoint[];
}

/** Hardware one-liner for a systems board row: GPU when present, else CPU + RAM. */
export function fmtHardware(hw: SystemsHardware | null | undefined): string {
  if (!hw) return "Unknown hardware";
  const parts: string[] = [];
  if (hw.gpu) {
    parts.push(hw.gpu + (hw.gpu_vram_gb ? ` (${hw.gpu_vram_gb} GB)` : ""));
    if (hw.cpu_model) parts.push(hw.cpu_model);
  } else if (hw.cpu_model) {
    parts.push(hw.cpu_model);
  }
  if (hw.ram_gb) parts.push(`${hw.ram_gb} GB RAM${hw.unified_memory ? " (unified)" : ""}`);
  return parts.length > 0 ? parts.join(" · ") : "Unknown hardware";
}

/** Colored usability badge for the model-fit matrix. */
export function usabilityBadgeEl(usability: string): HTMLElement {
  const cls =
    usability === "instant"
      ? "badge badge--ok"
      : usability === "usable"
        ? "badge badge--warn"
        : "badge badge--bad";
  const label =
    usability === "instant"
      ? "feels instant"
      : usability === "usable"
        ? "usable"
        : "painfully slow";
  return el("span", { class: cls }, label);
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
