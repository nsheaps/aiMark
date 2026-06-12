import { app } from "../src/app";

const port = Number(process.env.PORT ?? 8787);

console.log(`aimark-api listening on http://localhost:${port}`);

export default {
  port,
  fetch: app.fetch,
};
