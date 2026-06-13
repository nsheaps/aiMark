/**
 * mock-llm: a deterministic OpenAI-compatible streaming server for e2e CI.
 *
 * No model weights, no downloads — it streams canned tokens with configurable
 * pacing so the aimark CLI can exercise its full measurement path (TTFT,
 * inter-token timing, decode tps) against stable, fast, reproducible numbers.
 *
 *   bun tools/mock-llm/server.ts
 *
 * Env knobs:
 *   MOCK_PORT     (default 9090)
 *   MOCK_TTFT_MS  delay before the first content token   (default 20)
 *   MOCK_ITL_MS   inter-token latency in ms               (default 5)
 *   MOCK_TOKENS   content tokens per completion           (default 64)
 */

const port = Number(process.env.MOCK_PORT ?? 9090);
const ttftMs = Number(process.env.MOCK_TTFT_MS ?? 20);
const itlMs = Number(process.env.MOCK_ITL_MS ?? 5);
const tokensPerCompletion = Number(process.env.MOCK_TOKENS ?? 64);

const WORDS = "the quick brown fox jumps over the lazy dog and runs far away".split(" ");

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function sseChunk(id: string, model: string, delta: object, finish: string | null): string {
  return `data: ${JSON.stringify({
    id,
    object: "chat.completion.chunk",
    created: Math.floor(Date.now() / 1000),
    model,
    choices: [{ index: 0, delta, finish_reason: finish }],
  })}\n\n`;
}

async function handleChatCompletions(req: Request): Promise<Response> {
  const body = (await req.json().catch(() => null)) as {
    model?: string;
    stream?: boolean;
    max_tokens?: number;
  } | null;
  if (!body) return Response.json({ error: "invalid json" }, { status: 400 });

  const model = body.model ?? "mock-1";
  const id = `chatcmpl-mock-${Date.now()}`;
  const n = Math.min(tokensPerCompletion, body.max_tokens ?? tokensPerCompletion);
  const tokens = Array.from({ length: n }, (_, i) => `${WORDS[i % WORDS.length]} `);

  if (body.stream) {
    const stream = new ReadableStream({
      async start(controller) {
        const enc = new TextEncoder();
        controller.enqueue(enc.encode(sseChunk(id, model, { role: "assistant" }, null)));
        await sleep(ttftMs);
        for (const t of tokens) {
          controller.enqueue(enc.encode(sseChunk(id, model, { content: t }, null)));
          await sleep(itlMs);
        }
        controller.enqueue(enc.encode(sseChunk(id, model, {}, "stop")));
        controller.enqueue(enc.encode("data: [DONE]\n\n"));
        controller.close();
      },
    });
    return new Response(stream, {
      headers: { "content-type": "text/event-stream", "cache-control": "no-cache" },
    });
  }

  await sleep(ttftMs + n * itlMs);
  return Response.json({
    id,
    object: "chat.completion",
    created: Math.floor(Date.now() / 1000),
    model,
    choices: [
      {
        index: 0,
        message: { role: "assistant", content: tokens.join("") },
        finish_reason: "stop",
      },
    ],
    usage: { prompt_tokens: 16, completion_tokens: n, total_tokens: 16 + n },
  });
}

Bun.serve({
  port,
  async fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/v1/chat/completions" && req.method === "POST") {
      return handleChatCompletions(req);
    }
    if (url.pathname === "/v1/models") {
      return Response.json({ object: "list", data: [{ id: "mock-1", object: "model" }] });
    }
    if (url.pathname === "/health") {
      return Response.json({ ok: true, service: "mock-llm" });
    }
    return Response.json({ error: "not found" }, { status: 404 });
  },
});

console.log(`mock-llm listening on http://localhost:${port} (ttft=${ttftMs}ms itl=${itlMs}ms)`);
