import { sqliteTable, text, integer, real, primaryKey } from "drizzle-orm/sqlite-core";

/**
 * Drizzle schema for the aiMark API (SQLite dialect — bun:sqlite locally,
 * Cloudflare D1 in production). Kept deliberately pragmatic: envelopes are
 * stored whole as JSON alongside the indexed dimension columns the
 * leaderboard filters on.
 */

export const suites = sqliteTable(
  "suites",
  {
    id: text("id").notNull(),
    version: integer("version").notNull(),
    status: text("status").notNull(),
    manifestJson: text("manifest_json").notNull(),
  },
  (t) => [primaryKey({ columns: [t.id, t.version] })],
);

export const runs = sqliteTable("runs", {
  runId: text("run_id").primaryKey(),
  suiteId: text("suite_id").notNull(),
  suiteVersion: integer("suite_version").notNull(),
  track: text("track").notNull(),
  source: text("source").notNull(),
  /** accepted | flagged | rejected */
  status: text("status").notNull(),
  flagReason: text("flag_reason"),
  payloadSha256: text("payload_sha256").notNull().unique(),
  /** Timestamp the CLI recorded at run start (from the envelope). */
  createdAt: text("created_at").notNull(),
  /** Timestamp the API accepted the submission. */
  submittedAt: text("submitted_at").notNull(),
  ipHash: text("ip_hash").notNull(),
  claimTokenHash: text("claim_token_hash").notNull(),
  userId: text("user_id"),
  model: text("model").notNull(),
  runtime: text("runtime"),
  provider: text("provider"),
  quantization: text("quantization"),
  hardwareProfileId: text("hardware_profile_id"),
  paramsJson: text("params_json"),
  envelopeJson: text("envelope_json").notNull(),
  /** 0 = visible, 1 = hidden via the claim flow. */
  hidden: integer("hidden").notNull().default(0),
});

export const scores = sqliteTable(
  "scores",
  {
    runId: text("run_id").notNull(),
    name: text("name").notNull(),
    value: real("value").notNull(),
  },
  (t) => [primaryKey({ columns: [t.runId, t.name] })],
);

export const hardwareProfiles = sqliteTable("hardware_profiles", {
  id: text("id").primaryKey(),
  profileJson: text("profile_json").notNull(),
});

/** Phase 2 stub — GitHub OAuth users. Minimal on purpose. */
export const users = sqliteTable("users", {
  id: text("id").primaryKey(),
  githubLogin: text("github_login"),
  createdAt: text("created_at").notNull(),
});
