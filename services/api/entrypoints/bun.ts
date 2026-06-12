import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { Database } from "bun:sqlite";
import { drizzle } from "drizzle-orm/bun-sqlite";
import { createApp } from "../src/app";
import * as schema from "../src/db/schema";
import { migrate } from "../src/db/migrate";
import { seedSuites } from "../src/db/seed";
import { DEFAULT_RATE_LIMIT, InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";

const port = Number(process.env.PORT ?? 8787);
const dataDir = join(import.meta.dir, "..", "data");
mkdirSync(dataDir, { recursive: true });

const sqlite = new Database(process.env.AIMARK_DB_PATH ?? join(dataDir, "dev.sqlite"));
migrate(sqlite);
const db = drizzle(sqlite, { schema });
await seedSuites(db);

const rateLimit = Number(process.env.AIMARK_RATE_LIMIT ?? DEFAULT_RATE_LIMIT);

const app = createApp({
  db,
  blobs: new InMemoryBlobStore(),
  rateLimiter: new InMemoryRateLimiter(rateLimit),
  baseUrl: process.env.AIMARK_BASE_URL ?? `http://localhost:${port}`,
  adminToken: process.env.ADMIN_TOKEN,
});

console.log(`aimark-api listening on http://localhost:${port}`);

export default {
  port,
  fetch: app.fetch,
};
