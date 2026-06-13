/**
 * Canonical JSON + crypto helpers shared by the submission pipeline.
 *
 * canonicalize() must match the Go CLI byte-for-byte: recursively sorted
 * object keys, no whitespace, UTF-8. The HMAC dev key is the constant the
 * CLI embeds for key_gen "dev".
 */

export const DEV_HMAC_KEY = "aimark-dev-integrity-key-v0";
export const DEV_KEY_GEN = "dev";

export function canonicalize(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map((v) => canonicalize(v === undefined ? null : v)).join(",")}]`;
  }
  const record = value as Record<string, unknown>;
  const parts: string[] = [];
  for (const key of Object.keys(record).sort()) {
    const v = record[key];
    if (v === undefined) continue; // match JSON.stringify semantics
    parts.push(`${JSON.stringify(key)}:${canonicalize(v)}`);
  }
  return `{${parts.join(",")}}`;
}

const encoder = new TextEncoder();

function toHex(buffer: ArrayBuffer): string {
  return Array.from(new Uint8Array(buffer))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export async function sha256Hex(message: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(message));
  return toHex(digest);
}

export async function sha256HexBytes(data: Uint8Array): Promise<string> {
  // Copy so the view is plain-ArrayBuffer-backed (digest() rejects SharedArrayBuffer views).
  const digest = await crypto.subtle.digest("SHA-256", new Uint8Array(data));
  return toHex(digest);
}

export async function hmacSha256Hex(key: string, message: string): Promise<string> {
  const cryptoKey = await crypto.subtle.importKey(
    "raw",
    encoder.encode(key),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signature = await crypto.subtle.sign("HMAC", cryptoKey, encoder.encode(message));
  return toHex(signature);
}

export function randomHex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  crypto.getRandomValues(buf);
  return Array.from(buf)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
