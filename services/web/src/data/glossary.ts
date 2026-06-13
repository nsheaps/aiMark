// aiMark glossary — the novice-friendly dictionary for every AI term used on the site.
// Each entry has a one-sentence plain-English short_def (used by <Term> tooltips)
// and a longer 2-4 sentence long_def (shown on /docs/glossary/).

export type GlossaryCategory = "basics" | "performance" | "models" | "hardware" | "benchmarking";

export interface GlossaryEntry {
  id: string;
  term: string;
  short_def: string;
  long_def: string;
  category: GlossaryCategory;
}

export const categories: { id: GlossaryCategory; title: string; blurb: string }[] = [
  {
    id: "basics",
    title: "The basics",
    blurb: "Core ideas you need before anything else makes sense.",
  },
  {
    id: "models",
    title: "Models & runtimes",
    blurb: "What a model actually is, and the software that runs one.",
  },
  {
    id: "performance",
    title: "Performance & measurement",
    blurb: "The numbers aiMark measures and how to read them.",
  },
  {
    id: "hardware",
    title: "Hardware",
    blurb: "The parts of your machine that decide how fast a model runs.",
  },
  {
    id: "benchmarking",
    title: "Benchmarking & aiMark",
    blurb: "How aiMark turns measurements into comparable scores.",
  },
];

export const glossary: GlossaryEntry[] = [
  // ---------------------------------------------------------------- basics
  {
    id: "token",
    term: "Token",
    short_def:
      "A small chunk of text — roughly three-quarters of a word — that AI models read and write one at a time.",
    long_def:
      "AI models do not process whole words or sentences; they break text into tokens, which are common fragments like 'ing', 'the', or 'hello'. A rough rule of thumb is that 1,000 tokens is about 750 English words. Because models produce output one token at a time, almost every speed measurement in aiMark is expressed in tokens.",
    category: "basics",
  },
  {
    id: "llm",
    term: "LLM (Large Language Model)",
    short_def: "An AI program trained on huge amounts of text so it can read and write language.",
    long_def:
      "A large language model is the kind of AI behind chat assistants like ChatGPT and Claude. It is 'large' because it contains billions of learned numeric values (parameters) and was trained on enormous amounts of text. Given some input text, an LLM predicts what text should come next — which turns out to be enough to answer questions, write code, and summarize documents.",
    category: "basics",
  },
  {
    id: "inference",
    term: "Inference",
    short_def: "Running a trained AI model to get answers, as opposed to training it.",
    long_def:
      "Training is the expensive process of teaching a model; inference is everything after — actually using the model to produce output. When you chat with a model or run an aiMark benchmark, you are doing inference. aiMark measures inference performance: how fast and how consistently a given setup produces answers.",
    category: "basics",
  },
  {
    id: "prompt",
    term: "Prompt",
    short_def: "The text you send to a model — your question or instructions.",
    long_def:
      "A prompt is the input side of a model interaction: the question, instruction, or document you give the model to work with. Benchmark suites use fixed prompts so that every run measures the same work. Longer prompts take longer to process, which is why prompt length is controlled in every aiMark suite.",
    category: "basics",
  },
  {
    id: "completion",
    term: "Completion",
    short_def: "The text a model writes back in response to your prompt.",
    long_def:
      "The completion (also called the response or output) is what the model generates after reading your prompt. Models produce completions one token at a time. Most speed metrics, like tokens per second, describe how quickly the completion is generated.",
    category: "basics",
  },
  {
    id: "context-window",
    term: "Context window",
    short_def:
      "The maximum amount of text a model can pay attention to at once, measured in tokens.",
    long_def:
      "The context window is a model's working memory: the prompt plus the completion must fit inside it. A model with a 128k-token context window can consider roughly a 300-page book at once. Bigger windows let you work with longer documents but generally make processing slower and use more memory.",
    category: "basics",
  },
  {
    id: "system-prompt",
    term: "System prompt",
    short_def:
      "Hidden standing instructions that tell a model how to behave before the conversation starts.",
    long_def:
      "A system prompt is a special instruction given to the model ahead of the user's message — for example, 'You are a helpful assistant. Answer briefly.' It shapes tone, format, and rules for the whole conversation. Benchmark suites pin the system prompt (or use none) so all runs do identical work.",
    category: "basics",
  },
  {
    id: "temperature",
    term: "Temperature",
    short_def: "A setting that controls how random or predictable a model's word choices are.",
    long_def:
      "Temperature adjusts how adventurous the model is when picking each next token. At temperature 0 the model always picks its most likely choice, giving near-identical answers every time; higher values (like 0.8) produce more varied, creative output. aiMark suites use temperature 0 wherever determinism matters, so results are repeatable.",
    category: "basics",
  },
  {
    id: "max-tokens",
    term: "Max tokens",
    short_def: "A cap on how long the model's answer is allowed to be.",
    long_def:
      "Max tokens limits the length of the completion: when the model hits the cap, it stops mid-answer. Benchmarks set a fixed cap so no run can rack up a better-looking speed number by writing a different amount of text. Sprint 1, for example, caps completions at 256 tokens.",
    category: "basics",
  },
  {
    id: "streaming",
    term: "Streaming",
    short_def:
      "Receiving a model's answer word-by-word as it is generated, instead of waiting for the whole thing.",
    long_def:
      "With streaming, the model sends each token as soon as it is produced — that is why chat apps appear to 'type'. Streaming is essential for measuring responsiveness: it lets aiMark timestamp the first token (TTFT) and every token after it. All aiMark suites run with streaming enabled.",
    category: "basics",
  },
  {
    id: "seed",
    term: "Seed",
    short_def: "A starting number that makes a model's random choices repeatable.",
    long_def:
      "When a model samples tokens with some randomness, a seed fixes the random sequence so the same input produces the same output. Combined with temperature 0, seeds help make benchmark runs deterministic. Not every runtime or provider supports setting a seed.",
    category: "basics",
  },
  {
    id: "determinism",
    term: "Determinism",
    short_def: "When the same input always produces exactly the same output.",
    long_def:
      "A deterministic setup gives identical answers for identical prompts, which makes results easy to verify and compare. In practice, GPUs and serving software introduce small amounts of nondeterminism even at temperature 0. Benchmarks compensate by running many repetitions and reporting statistics rather than trusting any single run.",
    category: "basics",
  },
  {
    id: "hosted-api",
    term: "Hosted API",
    short_def: "A model running on a company's servers that you access over the internet.",
    long_def:
      "With a hosted API (Anthropic, OpenAI, Google, and others), the model runs in a provider's data center and you send requests over the network, usually paying per token. You get powerful models without owning hardware, but speed depends partly on the network and the provider's current load. aiMark puts hosted results on their own track so they are never silently compared with local runs.",
    category: "basics",
  },
  {
    id: "api-key",
    term: "API key",
    short_def:
      "A secret code that identifies you to a hosted AI service and lets it bill your account.",
    long_def:
      "An API key is like a password for programmatic access to a hosted model. The aimark CLI uses your key to run hosted benchmarks, and you pay the provider directly for the tokens used. aiMark never uploads your key — it stays on your machine and is excluded from result files.",
    category: "basics",
  },
  {
    id: "endpoint",
    term: "Endpoint",
    short_def: "The specific web address a program sends requests to in order to use a service.",
    long_def:
      "An endpoint is a URL that accepts a particular kind of request — for example, an endpoint that accepts a prompt and returns a completion. Local runtimes expose endpoints on your own machine (like http://localhost:11434); hosted providers expose them on the internet. The aimark CLI talks to model endpoints to run benchmarks.",
    category: "basics",
  },
  {
    id: "rate-limit",
    term: "Rate limit",
    short_def: "A cap on how many requests you can send to a service in a given time period.",
    long_def:
      "Hosted providers limit how fast each customer can send requests, both to keep service fair and to protect their systems. Hitting a rate limit causes requests to be rejected or delayed, which would distort benchmark numbers. The aimark CLI paces hosted runs to stay under your account's limits.",
    category: "basics",
  },
  {
    id: "prompt-caching",
    term: "Prompt caching",
    short_def:
      "A provider trick that skips re-reading parts of a prompt it has seen recently, making repeats faster and cheaper.",
    long_def:
      "When the start of your prompt is identical to a recent request, some providers reuse the earlier computation instead of redoing it. That can make repeated requests dramatically faster and cheaper — and would quietly inflate benchmark scores. aiMark records whether caching could apply and designs suites so repetitions measure real work.",
    category: "basics",
  },
  {
    id: "embedding",
    term: "Embedding",
    short_def: "A list of numbers that represents the meaning of a piece of text.",
    long_def:
      "An embedding converts text into a vector — a long list of numbers — so that texts with similar meanings end up numerically close together. Embeddings power semantic search and retrieval systems like RAG. Embedding models are separate from chat models and are not benchmarked in aiMark's v1 suites.",
    category: "basics",
  },
  {
    id: "fine-tuning",
    term: "Fine-tuning",
    short_def: "Additional training that adapts an existing model to a specific task or style.",
    long_def:
      "Instead of training a model from scratch, fine-tuning continues training an existing model on a smaller, focused dataset — for example, your company's support transcripts. The result is a new model variant with its own behavior and its own benchmark scores. Fine-tuned variants should be benchmarked separately from their base models.",
    category: "basics",
  },
  {
    id: "rag",
    term: "RAG (Retrieval-Augmented Generation)",
    short_def:
      "A technique where relevant documents are looked up and pasted into the prompt so the model can answer from them.",
    long_def:
      "RAG combines a search step with a generation step: a system retrieves documents related to the question and includes them in the prompt, so the model answers using real source material instead of just its training memory. It is the standard way to give models access to private or up-to-date information. RAG performance depends heavily on prompt processing speed, since retrieved documents make prompts long.",
    category: "basics",
  },
  {
    id: "hallucination",
    term: "Hallucination",
    short_def: "When a model confidently states something that is false or made up.",
    long_def:
      "Models generate plausible-sounding text, and sometimes plausible is all it is — invented citations, wrong dates, fake function names. Hallucination is a quality problem, not a speed problem. aiMark's quality suites use tasks with objectively checkable answers, so hallucinated answers simply score as wrong.",
    category: "basics",
  },
  {
    id: "ai-assistant",
    term: "AI assistant",
    short_def: "A chat product built on top of a language model, like ChatGPT or Claude.",
    long_def:
      "An assistant is the full product experience — chat interface, memory, tools, safety rules — wrapped around one or more underlying models. aiMark benchmarks the models and the systems serving them, not the assistant products. The same model can feel very different in different products depending on serving speed, which is exactly what aiMark measures.",
    category: "basics",
  },

  // ---------------------------------------------------------------- models & runtimes
  {
    id: "model",
    term: "Model",
    short_def:
      "The trained AI program itself — a big file of learned numbers plus the code to use them.",
    long_def:
      "A model is the artifact produced by training: billions of numeric parameters that together encode what the AI has learned. Models come in families (Llama, Qwen, GPT, Claude) and sizes, and the same model can be run by many different runtimes. On aiMark leaderboards, the model is one of the most important dimensions a score is sliced by.",
    category: "models",
  },
  {
    id: "weights",
    term: "Weights",
    short_def: "The learned numbers inside a model — the actual content of the model file.",
    long_def:
      "Weights (used interchangeably with 'parameters' in casual use) are the numeric values adjusted during training. Downloading a model really means downloading its weights. 'Open-weights' models publish these files so anyone can run them locally; closed models keep them on the provider's servers.",
    category: "models",
  },
  {
    id: "open-weights",
    term: "Open-weights model",
    short_def:
      "A model whose files are published so anyone can download and run it on their own hardware.",
    long_def:
      "Open-weights models — like Llama, Qwen, and Mistral releases — can be downloaded and run locally with tools like Ollama or llama.cpp. This is what makes aiMark's local track possible: anyone can benchmark the same model file on their own machine. 'Open weights' is not the same as open source; licenses vary.",
    category: "models",
  },
  {
    id: "parameter-count",
    term: "Parameter count",
    short_def:
      "How many learned numbers a model contains — the '8B' in Llama-3.1-8B means 8 billion.",
    long_def:
      "Parameter count is the standard measure of model size: 8B means 8 billion parameters. Bigger models are usually more capable but slower and hungrier for memory. Parameter count is captured on every aiMark run because comparing a 70B model's speed against an 8B model's is meaningless without knowing it.",
    category: "models",
  },
  {
    id: "quantization",
    term: "Quantization",
    short_def:
      "Shrinking a model by storing its numbers less precisely, trading a little quality for a lot of speed and memory.",
    long_def:
      "Models are trained with high-precision numbers, but those can be rounded down to smaller formats (8-bit, 4-bit) with surprisingly little quality loss. A 4-bit quantized model needs roughly a quarter of the memory and often runs much faster. Quantization level (like Q4_K_M) is captured on every aiMark run because it strongly affects both speed and quality scores.",
    category: "models",
  },
  {
    id: "gguf",
    term: "GGUF",
    short_def:
      "The standard file format for quantized models used by llama.cpp and tools built on it.",
    long_def:
      "GGUF is a single-file format that packages a model's quantized weights and metadata together. It is the format llama.cpp, Ollama, and LM Studio use for local models. The quantization level is encoded in the filename (for example, 'Q4_K_M'), and the file's hash serves as a fingerprint of exactly which model was benchmarked.",
    category: "models",
  },
  {
    id: "model-digest",
    term: "Model digest",
    short_def:
      "A fingerprint (hash) of a model file that proves exactly which model was benchmarked.",
    long_def:
      "A digest is a cryptographic hash of the model's contents — change one byte of the file and the digest changes completely. The aimark CLI records the digest (for example, from the Ollama manifest or a GGUF hash) so a leaderboard row provably refers to a specific model build, not just a name someone typed.",
    category: "models",
  },
  {
    id: "tokenizer",
    term: "Tokenizer",
    short_def: "The component that splits text into the tokens a model understands.",
    long_def:
      "Every model family ships a tokenizer that converts text to token IDs and back. Different tokenizers split the same sentence into different numbers of tokens, which means 'tokens per second' is only directly comparable between models with similar tokenizers. aiMark mitigates this by scoring against per-suite reference baselines rather than raw token counts alone.",
    category: "models",
  },
  {
    id: "transformer",
    term: "Transformer",
    short_def: "The neural-network design that nearly all modern language models are built on.",
    long_def:
      "Introduced in 2017, the transformer architecture processes all tokens in its input through layers of 'attention', letting the model weigh how every word relates to every other. Its structure is why prompt processing (parallel, fast) and token generation (sequential, slower) have such different performance characteristics — a split you will see all over aiMark's metrics.",
    category: "models",
  },
  {
    id: "attention",
    term: "Attention",
    short_def:
      "The mechanism that lets a model decide which earlier words matter for predicting the next one.",
    long_def:
      "Attention scores how relevant every token in the context is to the token currently being generated. It is the heart of the transformer architecture and also its main cost: attention work grows with context length, which is why long prompts slow models down and why context length is a controlled variable in benchmarks.",
    category: "models",
  },
  {
    id: "kv-cache",
    term: "KV cache",
    short_def:
      "A runtime's memory of the tokens it has already processed, so it never re-reads the whole conversation for each new token.",
    long_def:
      "While generating, the model stores intermediate results (keys and values) for every token it has seen. This KV cache makes generating each new token fast, but it consumes memory in proportion to context length. KV cache size is a major reason long-context inference needs lots of VRAM or unified memory.",
    category: "models",
  },
  {
    id: "mixture-of-experts",
    term: "Mixture of Experts (MoE)",
    short_def:
      "A model design that activates only a fraction of its parameters per token, getting big-model quality at lower compute cost.",
    long_def:
      "An MoE model contains many specialized sub-networks ('experts') and routes each token through just a few of them. A model might have 100B+ total parameters but only use ~10B per token, making it faster than its total size suggests. This is why aiMark captures both total and active parameter counts where runtimes report them.",
    category: "models",
  },
  {
    id: "distillation",
    term: "Distillation",
    short_def:
      "Training a small model to imitate a large one, keeping much of the quality at a fraction of the size.",
    long_def:
      "In distillation, a large 'teacher' model generates training data or signals for a small 'student' model. The student ends up far cheaper to run while retaining a surprising share of the teacher's ability. Many of the best small local models are distillations of much larger ones.",
    category: "models",
  },
  {
    id: "runtime",
    term: "Runtime",
    short_def: "The software that loads a model file and actually executes it on your hardware.",
    long_def:
      "A runtime (also called an inference engine or server) is the program that does the math: Ollama, llama.cpp, vLLM, and LM Studio are all runtimes. The same model can run several times faster on one runtime than another, on identical hardware. That is why aiMark records the runtime and its version on every local run — it is one of the biggest levers on your score.",
    category: "models",
  },
  {
    id: "ollama",
    term: "Ollama",
    short_def:
      "A popular, beginner-friendly tool for downloading and running AI models locally with one command.",
    long_def:
      "Ollama wraps model downloading, storage, and serving into a simple CLI: 'ollama run llama3.1' just works. Under the hood it builds on llama.cpp and exposes a local API that tools like aimark can benchmark against. Its manifest system also gives aiMark reliable model digests and quantization metadata.",
    category: "models",
  },
  {
    id: "llama-cpp",
    term: "llama.cpp",
    short_def:
      "A fast, lightweight open-source engine for running models on ordinary computers, including without a GPU.",
    long_def:
      "llama.cpp is the C/C++ project that made local LLMs practical on consumer hardware, pioneering aggressive quantization and CPU inference. It defined the GGUF format and powers many other tools, including Ollama and LM Studio. Enthusiasts often run it directly for maximum control over performance settings.",
    category: "models",
  },
  {
    id: "vllm",
    term: "vLLM",
    short_def:
      "A high-performance serving engine designed to handle many simultaneous requests on server GPUs.",
    long_def:
      "vLLM is built for throughput: it batches many concurrent requests together and manages GPU memory with a technique called PagedAttention. It is the common choice for production self-hosting on data-center GPUs. Expect vLLM to dominate concurrency-focused suites while simpler runtimes can be competitive for single-stream chat.",
    category: "models",
  },
  {
    id: "lm-studio",
    term: "LM Studio",
    short_def:
      "A desktop app with a graphical interface for downloading, chatting with, and serving local models.",
    long_def:
      "LM Studio gives local AI a friendly GUI: browse models, click to download, chat in-app, and optionally expose an OpenAI-compatible local server. That local server is what aimark benchmarks. Like Ollama, it builds on llama.cpp-family engines under the hood.",
    category: "models",
  },
  {
    id: "mlx",
    term: "MLX",
    short_def:
      "Apple's machine-learning framework, optimized for running models on Apple Silicon Macs.",
    long_def:
      "MLX is Apple's open-source array framework designed around the unified memory of M-series chips. MLX-based runtimes often outperform generic engines on Macs because they are written specifically for that hardware. aiMark reaches MLX runtimes through their OpenAI-compatible server endpoints.",
    category: "models",
  },
  {
    id: "openai-compatible",
    term: "OpenAI-compatible API",
    short_def:
      "A de-facto standard request format, copied from OpenAI's API, that most runtimes and providers support.",
    long_def:
      "OpenAI's chat API format became the lingua franca of model serving: vLLM, LM Studio, llama.cpp's server, Ollama, and OpenRouter all speak it. This is great for benchmarking — one aimark adapter can talk to all of them the same way. Provider-specific adapters are still used where extra metadata (like model digests) is available.",
    category: "models",
  },
  {
    id: "provider",
    term: "Provider",
    short_def: "The company operating a hosted model service — like Anthropic, OpenAI, or Google.",
    long_def:
      "On the hosted track, the provider is who actually serves the model: they own the hardware, set the prices, and determine real-world speed. The same open model can be offered by several providers at very different speeds and prices. aiMark records the provider and region on every hosted run.",
    category: "models",
  },

  // ---------------------------------------------------------------- performance
  {
    id: "ttft",
    term: "TTFT (Time To First Token)",
    short_def:
      "How long you wait between sending a prompt and seeing the first word of the answer.",
    long_def:
      "TTFT measures responsiveness: the gap between submitting a request and receiving the first token of the reply. It covers prompt processing plus any queueing, model loading, or network time. Low TTFT is what makes a chat feel instant — it is one of the highest-weighted metrics in the Sprint suite.",
    category: "performance",
  },
  {
    id: "tokens-per-second",
    term: "Tokens per second (tok/s)",
    short_def: "How many tokens a model generates each second — the headline 'speed' number.",
    long_def:
      "Tokens per second measures generation (decode) speed once the answer has started flowing. As a feel for the scale: 10 tok/s is around comfortable reading speed, while 100+ tok/s appears nearly instant for short answers. It is the single most quoted performance number for local models.",
    category: "performance",
  },
  {
    id: "inter-token-latency",
    term: "Inter-token latency",
    short_def: "The pause between each word appearing — the inverse of tokens per second.",
    long_def:
      "Inter-token latency is the average gap between consecutive tokens in a streamed response, typically a few to tens of milliseconds. It is effectively 1 ÷ (tokens per second) but is also studied for its variance: uneven gaps make streaming output feel stuttery even when the average speed is fine.",
    category: "performance",
  },
  {
    id: "latency",
    term: "Latency",
    short_def: "The total time from sending a request to receiving the complete answer.",
    long_def:
      "Latency (or end-to-end latency) is the full round trip: prompt processing, generation of every token, and network time. It is what you actually wait when you are not reading along with the stream. aiMark reports latency at several percentiles (p50, p95, p99) rather than just the average.",
    category: "performance",
  },
  {
    id: "percentile",
    term: "Percentile",
    short_def:
      "A way of describing results by rank: the p95 value is the time that 95% of requests beat.",
    long_def:
      "Instead of averaging all measurements — which hides bad moments — percentiles report the value at a given rank in the sorted results. The 95th percentile (p95) latency means 95 out of 100 requests were at least that fast, and 5 were slower. Percentiles are the standard way to describe how a system behaves at its worst, not just on average.",
    category: "performance",
  },
  {
    id: "p50",
    term: "p50 (median)",
    short_def: "The middle result — half of all requests were faster than this, half slower.",
    long_def:
      "p50 is the 50th percentile, better known as the median. It describes the typical experience and is more robust than the mean because a few extreme outliers cannot drag it around. aiMark uses p50 TTFT as a primary Sprint metric for exactly that reason.",
    category: "performance",
  },
  {
    id: "p95",
    term: "p95",
    short_def: "The time beaten by 95% of requests — a measure of your 'bad moments'.",
    long_def:
      "p95 captures the slow tail: one request in twenty is slower than this value. A system with great p50 but terrible p95 feels fast most of the time and frustrating often enough to notice. Sprint 1 scores p95 latency directly, so consistency under occasional slowness counts.",
    category: "performance",
  },
  {
    id: "p99",
    term: "p99",
    short_def: "The time beaten by 99% of requests — the worst-case-ish number.",
    long_def:
      "p99 describes the slowest 1% of requests. It matters most under load and at scale, where rare slow requests happen constantly in absolute terms. Throughput-oriented suites like Marathon emphasize p99 under concurrency.",
    category: "performance",
  },
  {
    id: "coefficient-of-variation",
    term: "Coefficient of variation (CV)",
    short_def:
      "A 0-to-1-ish number describing how spread out measurements are relative to their average — lower means steadier.",
    long_def:
      "CV is the standard deviation divided by the mean. A CV of 0.05 means results wobble about 5% around their average — very steady; a CV of 0.5 means wildly inconsistent. aiMark's Consistency sub-score is built on CV: two setups with the same average speed can earn very different scores if one is much steadier.",
    category: "performance",
  },
  {
    id: "throughput",
    term: "Throughput",
    short_def: "Total work completed per unit time across all simultaneous requests, not just one.",
    long_def:
      "While tokens per second usually describes a single stream, throughput sums output across every concurrent request a system is serving. A server might deliver 30 tok/s per user while sustaining 500 tok/s total across 32 users. Throughput is the headline number for serving infrastructure and the focus of the Marathon suite.",
    category: "performance",
  },
  {
    id: "concurrency",
    term: "Concurrency",
    short_def: "How many requests are being processed at the same time.",
    long_def:
      "Concurrency 1 means one request at a time; concurrency 16 means sixteen in flight together. Higher concurrency raises total throughput but typically slows each individual stream. Benchmark results are only comparable at the same concurrency level, so aiMark captures it on every run.",
    category: "performance",
  },
  {
    id: "prefill",
    term: "Prefill",
    short_def:
      "The first phase of a request, where the model reads and digests your entire prompt.",
    long_def:
      "Before generating anything, the model processes all prompt tokens in parallel — the prefill phase. Prefill speed (prompt tokens per second) determines how quickly long documents are ingested and is the main component of TTFT. It is usually much faster per token than generation, because it parallelizes well.",
    category: "performance",
  },
  {
    id: "decode",
    term: "Decode",
    short_def:
      "The second phase of a request, where the model generates the answer one token at a time.",
    long_def:
      "After prefill, the model enters decode: producing output tokens sequentially, each one depending on all the ones before. Decode speed is what 'tokens per second' usually refers to, and it is limited mainly by memory bandwidth. Sprint 1's highest-weighted metric is mean decode speed.",
    category: "performance",
  },
  {
    id: "batch-size",
    term: "Batch size",
    short_def: "How many requests a runtime processes together in one pass through the model.",
    long_def:
      "Runtimes can stack multiple requests and push them through the model simultaneously, which uses the hardware far more efficiently. Larger batches raise total throughput at some cost to individual latency. Serving engines like vLLM batch continuously and automatically.",
    category: "performance",
  },
  {
    id: "cold-start",
    term: "Cold start",
    short_def:
      "The extra delay on the first request because the model was not yet loaded and ready.",
    long_def:
      "If a model is not in memory, the first request pays for loading it from disk — seconds to minutes depending on size. Such cold starts would contaminate timing statistics, which is why aiMark suites send warmup requests first and discard them. Hosted APIs can have their own milder cold-start effects on rarely used models.",
    category: "performance",
  },
  {
    id: "load-time",
    term: "Model load time",
    short_def:
      "How long it takes to read a model from disk into memory before it can answer anything.",
    long_def:
      "Loading copies the model's weights from disk into RAM or VRAM, taking anywhere from a second to several minutes for large models. It is a one-time cost per session, not a per-request cost, so aiMark measures it separately from request timing where the runtime reports it (Ollama does).",
    category: "performance",
  },
  {
    id: "network-latency",
    term: "Network latency",
    short_def:
      "Time spent moving data over the internet, which inflates hosted-API timing measurements.",
    long_def:
      "Every hosted request pays a network round trip before the provider even starts working — typically tens of milliseconds. This overhead is part of TTFT for hosted runs, which is one reason hosted and local results live on separate tracks. The aimark CLI measures a network round-trip preflight so this component can be reported alongside results.",
    category: "performance",
  },

  // ---------------------------------------------------------------- hardware
  {
    id: "gpu",
    term: "GPU (Graphics Processing Unit)",
    short_def:
      "The chip — originally for graphics — that does the heavy parallel math of AI, much faster than a CPU.",
    long_def:
      "GPUs contain thousands of small cores designed to do the same operation on lots of data at once, which is exactly the shape of neural-network math. For most setups, the GPU (and especially its memory) is the single biggest factor in local AI performance. aiMark's hardware profiles capture GPU model and VRAM on every run.",
    category: "hardware",
  },
  {
    id: "cpu",
    term: "CPU (Central Processing Unit)",
    short_def:
      "Your computer's main general-purpose processor, which can run models too — just more slowly.",
    long_def:
      "The CPU executes everything on your computer and can also run model inference when no suitable GPU is available. CPU inference is far slower for large models but works everywhere and is practical for small models. CPU model and core count are part of every aiMark hardware profile.",
    category: "hardware",
  },
  {
    id: "ram",
    term: "RAM",
    short_def: "Your computer's main working memory, where models live during CPU inference.",
    long_def:
      "RAM is the fast, temporary memory the CPU works from. To run a model on the CPU, its weights must fit in RAM — a 4-bit 8B model needs roughly 5GB. Systems with unified memory (like Apple Silicon) share one pool between CPU and GPU, blurring the RAM/VRAM distinction.",
    category: "hardware",
  },
  {
    id: "vram",
    term: "VRAM",
    short_def:
      "The memory on your graphics card — the main limit on how big a model you can run on GPU.",
    long_def:
      "VRAM (video RAM) is dedicated high-speed memory attached to a GPU. A model runs at full GPU speed only if its weights and working state fit in VRAM; an 8GB card handles 4-bit 8B models comfortably, while 70B models need many times more. When a model does not fit, layers spill to system RAM and performance drops sharply.",
    category: "hardware",
  },
  {
    id: "unified-memory",
    term: "Unified memory",
    short_def: "A single pool of memory shared by CPU and GPU, used by Apple Silicon Macs.",
    long_def:
      "On Apple's M-series chips, the CPU and GPU share one fast memory pool instead of having separate RAM and VRAM. A Mac with 64GB of unified memory can run models that would need a very expensive dedicated GPU elsewhere. aiMark detects unified memory and records it distinctly in hardware profiles, since it changes what 'fits' means.",
    category: "hardware",
  },
  {
    id: "gpu-offload",
    term: "GPU offload",
    short_def:
      "Splitting a model between GPU and CPU when it does not fully fit in graphics memory.",
    long_def:
      "Runtimes can place some of a model's layers on the GPU and keep the rest on the CPU — 'offloading 30 of 40 layers'. Each layer left on the CPU slows generation, so partially offloaded results differ greatly from fully-on-GPU ones. The offload configuration is part of what aiMark captures about a run.",
    category: "hardware",
  },
  {
    id: "cpu-inference",
    term: "CPU inference",
    short_def: "Running a model entirely on the processor, with no graphics card involved.",
    long_def:
      "CPU inference works on any computer and is how many people first try local models. It is memory-bandwidth-bound and much slower than GPU inference for big models, but small quantized models can still reach usable speeds. llama.cpp made CPU inference practical and remains the reference implementation for it.",
    category: "hardware",
  },
  {
    id: "memory-bandwidth",
    term: "Memory bandwidth",
    short_def:
      "How fast data moves between memory and processor — the real speed limit for generating tokens.",
    long_def:
      "Generating each token requires reading essentially the whole model from memory, so generation speed is usually limited by memory bandwidth rather than raw compute. This is why GPUs (very high bandwidth) beat CPUs, and why Apple Silicon's fast unified memory punches above its weight. As a rule of thumb: max tok/s ≈ bandwidth ÷ model size.",
    category: "hardware",
  },
  {
    id: "apple-silicon",
    term: "Apple Silicon",
    short_def:
      "Apple's M-series Mac chips, whose shared fast memory makes them surprisingly good at running local models.",
    long_def:
      "Apple Silicon (M1 through M4 families) combines CPU, GPU, and a single pool of high-bandwidth unified memory on one chip. High-memory configurations can run very large models that would otherwise require expensive server GPUs. They are a major hardware class on aiMark's local leaderboards, detected with unified-memory awareness.",
    category: "hardware",
  },
  {
    id: "hardware-profile",
    term: "Hardware profile",
    short_def:
      "The recorded snapshot of the machine a benchmark ran on — CPU, RAM, GPU, VRAM, and OS.",
    long_def:
      "Every local aiMark run captures a hardware profile: CPU model and cores, total RAM, GPU and VRAM (or unified memory), and operating system. The canonical fields are hashed into a hardware_profile_id, so runs from identical rigs cluster together on the leaderboard. Detection failures degrade to 'unknown' rather than blocking a run.",
    category: "hardware",
  },
  {
    id: "thermal-throttling",
    term: "Thermal throttling",
    short_def:
      "When a hot chip slows itself down to cool off, making later measurements slower than earlier ones.",
    long_def:
      "Sustained AI workloads generate serious heat, and chips respond by reducing their speed. A laptop may benchmark brilliantly for thirty seconds and then sag — which shows up as drift across repetitions. The Consistency sub-score naturally penalizes throttling-induced variance, and that is by design.",
    category: "hardware",
  },

  // ---------------------------------------------------------------- benchmarking
  {
    id: "benchmark",
    term: "Benchmark",
    short_def: "A standardized, repeatable test that lets different systems be compared fairly.",
    long_def:
      "A benchmark fixes the workload — the same tasks, settings, and measurement rules for everyone — so differences in results reflect the systems being tested, not differences in the test. aiMark is a benchmark for AI inference, in the same spirit that 3DMark is a benchmark for graphics hardware.",
    category: "benchmarking",
  },
  {
    id: "suite",
    term: "Suite",
    short_def:
      "A named, versioned collection of benchmark tasks that produces one score — like Sprint or Marathon.",
    long_def:
      "Each aiMark suite targets one aspect of AI performance: Sprint measures interactive chat feel, Marathon measures sustained throughput, Gauntlet measures answer quality. A suite freezes its tasks, prompts, settings, and scoring rules per version. Your score is always tied to a specific suite and version.",
    category: "benchmarking",
  },
  {
    id: "suite-version",
    term: "Suite version",
    short_def:
      "A frozen edition of a suite — when anything changes, the version bumps and the leaderboard resets.",
    long_def:
      "Like 3DMark generations, a suite version freezes everything that could affect scores: tasks, prompts, grading, reference baselines, weights, and protocol. Any change creates a new version with its own fresh leaderboard, so old scores are never silently invalidated. Sprint 1 means version 1 of the Sprint suite.",
    category: "benchmarking",
  },
  {
    id: "task",
    term: "Task",
    short_def: "One individual prompt-and-measure unit inside a suite.",
    long_def:
      "A suite is built from tasks — for Sprint 1, ten short prompts like 'summarize this sentence' or 'translate this phrase'. Each task is run many times (repetitions) and its measurements feed the suite's metrics. Tasks are frozen per suite version so every run does identical work.",
    category: "benchmarking",
  },
  {
    id: "sub-score",
    term: "Sub-score",
    short_def:
      "A score for one dimension of performance — Performance, Quality, Cost-Efficiency, or Consistency.",
    long_def:
      "aiMark separates 'how fast', 'how good', 'how cheap', and 'how steady' into four sub-scores, each on the same 1000-point reference scale. Not every suite produces every sub-score — Sprint 1 produces Performance and Consistency. Sub-scores combine into the composite aiMark Score via a weighted geometric mean.",
    category: "benchmarking",
  },
  {
    id: "composite-score",
    term: "Composite score (aiMark Score)",
    short_def: "The single headline number that combines all sub-scores for a run.",
    long_def:
      "The composite aiMark Score is the weighted geometric mean of a run's applicable sub-scores. The geometric mean is used so no single dimension can dominate: doubling one sub-score while halving another leaves the composite roughly unchanged. Like every aiMark number, ~1000 means 'matches the reference baseline'.",
    category: "benchmarking",
  },
  {
    id: "reference-baseline",
    term: "Reference baseline",
    short_def: "The fixed yardstick setup that defines 1000 points for a suite version.",
    long_def:
      "Each suite version ships frozen reference values for every metric — the 3DMark trick. Score 1000 means you matched the reference exactly; 2000 means roughly twice as good. Sprint 1 anchors to round-number synthetic baselines (100 tok/s decode, 50ms TTFT, 1500ms p95, CV 0.10); a future version will re-anchor to a measured reference rig, resetting the board.",
    category: "benchmarking",
  },
  {
    id: "geometric-mean",
    term: "Geometric mean",
    short_def:
      "An average computed by multiplying values instead of adding them, so no single huge value can dominate.",
    long_def:
      "The geometric mean multiplies n values together and takes the n-th root. Unlike the ordinary average, it treats ratios symmetrically: doubling one input and halving another cancels out. aiMark uses weighted geometric means at every level of scoring so a setup cannot buy a high composite by maxing one metric while tanking the rest.",
    category: "benchmarking",
  },
  {
    id: "track",
    term: "Track",
    short_def:
      "A separate leaderboard division — local (your hardware) vs hosted (provider APIs) — that are never silently mixed.",
    long_def:
      "Local runs measure your machine; hosted runs measure a provider's service over the internet. The numbers mean different things — hosted timing includes network, local includes your GPU — so each suite keeps separate local and hosted leaderboards. Cross-track comparison is allowed only explicitly in the compare view, with caveats shown.",
    category: "benchmarking",
  },
  {
    id: "leaderboard",
    term: "Leaderboard",
    short_def: "The public ranking of submitted scores for one suite version and track.",
    long_def:
      "Every accepted submission lands on the leaderboard for its (suite, version, track), ranked by composite score. Boards are sliceable by any captured dimension — model, runtime, quantization, hardware — and by default exclude flagged and CI-generated runs. Scores from different suite versions or tracks are never ranked against each other.",
    category: "benchmarking",
  },
  {
    id: "warmup",
    term: "Warmup",
    short_def:
      "Throwaway requests sent before measuring starts, so loading and caching effects don't pollute the results.",
    long_def:
      "The first requests to a model often pay one-time costs: loading weights from disk, compiling kernels, filling caches. aiMark suites send a fixed number of warmup requests (Sprint 1 sends 3) and discard their timings entirely. Only the steady-state repetitions after warmup count toward your score.",
    category: "benchmarking",
  },
  {
    id: "repetition",
    term: "Repetition",
    short_def:
      "One measured pass of a task — suites run many and report statistics, never a single timing.",
    long_def:
      "A single measurement can be lucky or unlucky, so aiMark suites repeat each task many times (Sprint 1 runs 20 repetitions after warmup). The repetitions feed the percentiles and variance statistics that scores are built from. More repetitions mean more reliable numbers at the cost of a longer run.",
    category: "benchmarking",
  },
  {
    id: "sample",
    term: "Sample",
    short_def: "One raw measured data point — a single request's recorded timings.",
    long_def:
      "Each repetition produces a sample: the per-request record of timestamps, token counts, and timings. Aggregate metrics like p95 latency are computed across all samples of a run. The aimark CLI keeps raw samples in your local result file and uploads them as artifacts on submit, so scores can be re-derived and audited.",
    category: "benchmarking",
  },
  {
    id: "metric",
    term: "Metric",
    short_def: "A single named measurement a suite scores — like mean decode speed or p50 TTFT.",
    long_def:
      "Metrics are the bridge between raw samples and scores: each suite defines which metrics it computes (for example, decode_tps_mean or ttft_ms_p50), each with a frozen reference value and weight. A run's metric values are divided by their references and combined into sub-scores. All metrics for a run are shown on its detail page with units.",
    category: "benchmarking",
  },
  {
    id: "sweep",
    term: "Sweep",
    short_def:
      "An automated series of runs that varies parameters one at a time to see which ones move the score.",
    long_def:
      "A sweep takes a matrix of settings — say, three models × four context lengths — and runs an independent scored benchmark for every combination. All cells share a sweep_id so they can be analyzed together. Sweeps power aiMark's parameter-impact explorer, which answers 'which settings actually matter?'.",
    category: "benchmarking",
  },
  {
    id: "run",
    term: "Run",
    short_def:
      "One complete execution of a suite against one target — producing one score and one leaderboard row.",
    long_def:
      "A run is the unit of submission: the aimark CLI executes every task and repetition of a suite against a single model/runtime/hardware combination, computes metrics and provisional scores, and writes a result file. Submitting the run sends it to the API, which recomputes the score canonically and publishes it. Every leaderboard row links to its run's detail page.",
    category: "benchmarking",
  },
  {
    id: "target",
    term: "Target",
    short_def:
      "The specific model-on-a-runtime combination a benchmark is pointed at, like ollama:llama3.1:8b.",
    long_def:
      "The target tells the aimark CLI what to benchmark: a runtime or provider plus a model identifier. 'ollama:llama3.1:8b' targets the llama3.1:8b model served by your local Ollama. One machine can produce many runs against many targets.",
    category: "benchmarking",
  },
  {
    id: "plausibility-check",
    term: "Plausibility check",
    short_def:
      "An automatic sanity test that flags physically impossible or wildly unusual results.",
    long_def:
      "Every submission is checked against per-metric bounds (a laptop claiming 50,000 tok/s is not plausible) and against the statistical distribution of comparable runs. Results that are impossible or extreme outliers (beyond ~4 standard deviations) are flagged: still visible, but excluded from default leaderboards. Statistical detection is aiMark's primary defense against gamed scores.",
    category: "benchmarking",
  },
  {
    id: "hmac-signing",
    term: "HMAC signing",
    short_def:
      "A cryptographic stamp the CLI puts on results so casually forged submissions are rejected.",
    long_def:
      "The official aimark binaries sign each result payload with a secret key (an HMAC), and the API verifies the signature on submission. Because the key ships inside a client-side binary, a determined person can extract it — aiMark documents this honestly. Signing raises the bar against casual tampering; statistical plausibility detection is the real defense.",
    category: "benchmarking",
  },
  {
    id: "integrity-tier",
    term: "Integrity tier",
    short_def:
      "A run's trust level: unverified (anonymous), claimed (GitHub-linked), or verified (signed and plausible).",
    long_def:
      "Each run carries a tier reflecting how much can be vouched for it: unverified anonymous submissions, claimed runs tied to a GitHub account, and verified runs that passed signature and plausibility checks. Default leaderboard views favor verified results. No tier implies cryptographic proof — see HMAC signing for the honest framing.",
    category: "benchmarking",
  },
  {
    id: "claim-token",
    term: "Claim token",
    short_def:
      "A secret code returned for an anonymous submission that lets you later prove the run is yours.",
    long_def:
      "Submit without logging in and the API returns a claim token alongside your run ID. Keep it: after signing in with GitHub later, the token lets you attach the run to your profile. Anyone without the token cannot claim your run.",
    category: "benchmarking",
  },
  {
    id: "anonymous-submission",
    term: "Anonymous submission",
    short_def: "Publishing a score without creating an account or logging in.",
    long_def:
      "aiMark does not require an account: anyone can submit runs anonymously, and they appear on leaderboards in the unverified tier. Each anonymous submission returns a claim token so the run can be attached to a GitHub identity later. Light rate limits apply to anonymous submitters.",
    category: "benchmarking",
  },
  {
    id: "ci-run",
    term: "CI run",
    short_def:
      "A benchmark submitted automatically by aiMark's own test infrastructure rather than by a person.",
    long_def:
      "aiMark's continuous-integration pipelines submit real runs to exercise the system end to end. They are labelled source=ci and excluded from default leaderboards, since shared CI machines are slow and noisy — their numbers say nothing about real hardware. You can reveal them with the 'show CI runs' toggle on any board.",
    category: "benchmarking",
  },
  {
    id: "flagged-run",
    term: "Flagged run",
    short_def:
      "A run held out of default leaderboards because it failed a plausibility or integrity check.",
    long_def:
      "Flagged runs remain publicly visible — transparency over silent deletion — but are excluded from default board views and marked with a badge. Common causes are implausible metric values, statistical-outlier detection, or failed signature checks. The 'show flagged' toggle reveals them.",
    category: "benchmarking",
  },
  {
    id: "dedup",
    term: "Deduplication",
    short_def: "Detecting and rejecting the same result submitted more than once.",
    long_def:
      "Every submission carries a hash of its exact payload, and the API enforces uniqueness on it — resubmitting the same result file is a no-op. Timestamps and one-time values in the payload prevent replaying old results as new ones. This keeps duplicate rows off the leaderboards.",
    category: "benchmarking",
  },
  {
    id: "region",
    term: "Region",
    short_def: "The geographic location of the provider data center serving a hosted run.",
    long_def:
      "Hosted performance varies by where the request lands: a model can be faster in one provider region than another, and your distance to the region adds network latency. aiMark captures the region on hosted runs so leaderboards can be sliced by it. Local runs have no region.",
    category: "benchmarking",
  },
  {
    id: "cost-per-token",
    term: "Cost per token",
    short_def:
      "What a hosted provider charges for each token read or written, usually quoted per million tokens.",
    long_def:
      "Hosted APIs bill by usage, with separate prices for input (prompt) and output (completion) tokens — for example, $3 per million input tokens. The aimark CLI snapshots the provider's pricing at run time, so cost numbers stay accurate even after prices change. Cost feeds the Cost-Efficiency sub-score on the hosted track.",
    category: "benchmarking",
  },
  {
    id: "cost-efficiency",
    term: "Cost-Efficiency",
    short_def: "The sub-score measuring how much quality you get per dollar spent.",
    long_def:
      "Cost-Efficiency relates benchmark quality to what the run cost: quality-per-dollar. It applies to the hosted track, where pricing snapshots are captured on every run; local runs have no direct cost (optional energy metering may come later). A cheap model that scores nearly as well as an expensive one wins this dimension.",
    category: "benchmarking",
  },
  {
    id: "outlier",
    term: "Outlier",
    short_def:
      "A measurement far outside the normal range — either a fluke or a sign something is wrong.",
    long_def:
      "Outliers are values that sit far from the rest of their distribution, like one 9-second response among twenty 1-second ones. Within a run, percentile-based metrics keep single outliers from dominating the score. Across the corpus, runs that are extreme outliers versus comparable submissions get flagged for review.",
    category: "benchmarking",
  },
  {
    id: "result-file",
    term: "Result file",
    short_def: "The local JSON file the CLI writes after each run, containing everything about it.",
    long_def:
      "Each run produces a result file on your machine (schema aimark.run.v1) holding the environment capture, every raw sample, computed metrics, provisional scores, and the integrity signature. Runs are offline-first: benchmark now, inspect the file, and submit whenever you like with 'aimark submit'. Nothing leaves your machine until you submit.",
    category: "benchmarking",
  },
  {
    id: "envelope",
    term: "Envelope",
    short_def:
      "The descriptive wrapper of a result file: which CLI, suite, target, and environment produced the numbers.",
    long_def:
      "The envelope is the who/what/where of a run: CLI version, suite and version, target details, and the captured environment including the hardware profile. It is what makes a score interpretable — the same numbers mean different things on different setups. Run detail pages render the envelope alongside the scores.",
    category: "benchmarking",
  },
  {
    id: "parameter-impact",
    term: "Parameter impact",
    short_def:
      "How much changing one setting — like quantization or context length — moves benchmark scores on average.",
    long_def:
      "Because every run captures its full settings vector, aiMark can compute effect sizes across all submissions: 'going from 8-bit to 4-bit quantization changes Sprint scores by X% on average'. This corpus-level analysis is surfaced in the parameter-impact explorer. It turns thousands of individual runs into general answers about what matters.",
    category: "benchmarking",
  },
];

const ids = new Set<string>();
for (const entry of glossary) {
  if (ids.has(entry.id)) throw new Error(`Duplicate glossary id: ${entry.id}`);
  ids.add(entry.id);
}

export function getTerm(id: string): GlossaryEntry {
  const entry = glossary.find((g) => g.id === id);
  if (!entry) throw new Error(`Unknown glossary term id: "${id}"`);
  return entry;
}
