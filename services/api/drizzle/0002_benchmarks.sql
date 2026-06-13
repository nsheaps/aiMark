CREATE TABLE `programs` (
	`id` text NOT NULL,
	`version` integer NOT NULL,
	`status` text NOT NULL,
	`manifest_json` text NOT NULL,
	PRIMARY KEY(`id`, `version`)
);
--> statement-breakpoint
CREATE TABLE `benchmarks` (
	`bench_id` text PRIMARY KEY NOT NULL,
	`program_id` text NOT NULL,
	`program_version` integer NOT NULL,
	`class` text NOT NULL,
	`source` text NOT NULL,
	`status` text NOT NULL,
	`flag_reason` text,
	`payload_sha256` text NOT NULL,
	`created_at` text NOT NULL,
	`submitted_at` text NOT NULL,
	`ip_hash` text NOT NULL,
	`claim_token_hash` text NOT NULL,
	`hidden` integer DEFAULT 0 NOT NULL,
	`hardware_profile_id` text,
	`envelope_json` text NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `benchmarks_payload_sha256_unique` ON `benchmarks` (`payload_sha256`);--> statement-breakpoint
CREATE TABLE `benchmark_scores` (
	`bench_id` text NOT NULL,
	`name` text NOT NULL,
	`value` real NOT NULL,
	PRIMARY KEY(`bench_id`, `name`)
);
