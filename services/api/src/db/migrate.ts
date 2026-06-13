import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import type { Database as BunSqliteDatabase } from "bun:sqlite";

/**
 * Minimal migration runner for bun:sqlite (dev + tests). Applies the SQL
 * files drizzle-kit generated into services/api/drizzle/ in lexical order,
 * tracking applied files in a local bookkeeping table.
 *
 * Production (Cloudflare D1) does NOT use this — the deploy workflow applies
 * the same files with `wrangler d1 migrations apply` (see drizzle/README.md).
 */

const MIGRATIONS_DIR = join(import.meta.dir, "..", "..", "drizzle");
const STATEMENT_BREAKPOINT = "--> statement-breakpoint";

export function migrate(sqlite: BunSqliteDatabase, migrationsDir: string = MIGRATIONS_DIR): void {
  sqlite.run(
    "CREATE TABLE IF NOT EXISTS __aimark_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)",
  );
  const applied = new Set(
    (sqlite.query("SELECT name FROM __aimark_migrations").all() as { name: string }[]).map(
      (r) => r.name,
    ),
  );
  const files = readdirSync(migrationsDir)
    .filter((f) => f.endsWith(".sql"))
    .sort();
  for (const file of files) {
    if (applied.has(file)) continue;
    const sql = readFileSync(join(migrationsDir, file), "utf-8");
    for (const statement of sql.split(STATEMENT_BREAKPOINT)) {
      const trimmed = statement.trim();
      if (trimmed.length > 0) sqlite.run(trimmed);
    }
    sqlite
      .query("INSERT INTO __aimark_migrations (name, applied_at) VALUES (?, ?)")
      .run(file, new Date().toISOString());
  }
}
