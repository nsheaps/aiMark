import { defineConfig } from "astro/config";

// Static output: the built site is served as Cloudflare Worker static assets
// alongside the API (see services/api/wrangler.toml [assets]).
export default defineConfig({
  output: "static",
  site: "https://aimark.dev",
});
