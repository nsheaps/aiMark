CREATE TABLE `artifacts` (
	`key` text PRIMARY KEY NOT NULL,
	`run_id` text NOT NULL,
	`kind` text NOT NULL,
	`sha256` text NOT NULL,
	`size` integer NOT NULL,
	`created_at` text NOT NULL
);
