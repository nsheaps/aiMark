CREATE TABLE `hardware_profiles` (
	`id` text PRIMARY KEY NOT NULL,
	`profile_json` text NOT NULL
);
--> statement-breakpoint
CREATE TABLE `runs` (
	`run_id` text PRIMARY KEY NOT NULL,
	`suite_id` text NOT NULL,
	`suite_version` integer NOT NULL,
	`track` text NOT NULL,
	`source` text NOT NULL,
	`status` text NOT NULL,
	`flag_reason` text,
	`payload_sha256` text NOT NULL,
	`created_at` text NOT NULL,
	`submitted_at` text NOT NULL,
	`ip_hash` text NOT NULL,
	`claim_token_hash` text NOT NULL,
	`user_id` text,
	`model` text NOT NULL,
	`runtime` text,
	`provider` text,
	`quantization` text,
	`hardware_profile_id` text,
	`params_json` text,
	`envelope_json` text NOT NULL,
	`hidden` integer DEFAULT 0 NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `runs_payload_sha256_unique` ON `runs` (`payload_sha256`);--> statement-breakpoint
CREATE TABLE `scores` (
	`run_id` text NOT NULL,
	`name` text NOT NULL,
	`value` real NOT NULL,
	PRIMARY KEY(`run_id`, `name`)
);
--> statement-breakpoint
CREATE TABLE `suites` (
	`id` text NOT NULL,
	`version` integer NOT NULL,
	`status` text NOT NULL,
	`manifest_json` text NOT NULL,
	PRIMARY KEY(`id`, `version`)
);
--> statement-breakpoint
CREATE TABLE `users` (
	`id` text PRIMARY KEY NOT NULL,
	`github_login` text,
	`created_at` text NOT NULL
);
