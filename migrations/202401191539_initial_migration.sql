CREATE TABLE `apps` (`id` integer,`name` text,`description` text,`nostr_pubkey` text UNIQUE,`created_at` datetime,`updated_at` datetime,PRIMARY KEY (`id`));
CREATE TABLE `app_permissions` (`id` integer,`app_id` integer,`request_method` text,`max_amount` integer,`budget_renewal` text,`expires_at` datetime,`created_at` datetime,`updated_at` datetime,PRIMARY KEY (`id`),CONSTRAINT `fk_app_permissions_app` FOREIGN KEY (`app_id`) REFERENCES `apps`(`id`) ON DELETE CASCADE);
CREATE INDEX `idx_app_permissions_request_method` ON `app_permissions`(`request_method`);
CREATE INDEX `idx_app_permissions_app_id` ON `app_permissions`(`app_id`);
CREATE TABLE `payments` (`id` integer,`app_id` integer,`nostr_event_id` integer,`amount` integer,`payment_request` text,`preimage` text,`created_at` datetime,`updated_at` datetime, `preimage2` text,PRIMARY KEY (`id`),CONSTRAINT `fk_payments_app` FOREIGN KEY (`app_id`) REFERENCES `apps`(`id`) ON DELETE CASCADE,CONSTRAINT `fk_payments_nostr_event` FOREIGN KEY (`nostr_event_id`) REFERENCES `nostr_events`(`id`));
CREATE INDEX `idx_payments_nostr_event_id` ON `payments`(`nostr_event_id`);
CREATE INDEX `idx_payments_app_id` ON `payments`(`app_id`);
CREATE TABLE "nostr_events" (`id` integer,`app_id` integer null,`nostr_id` text UNIQUE,`kind` integer,`pub_key` text,`content` text,`replied_at` datetime,`created_at` datetime,`updated_at` datetime,PRIMARY KEY (`id`),CONSTRAINT `fk_nostr_events_app` FOREIGN KEY (`app_id`) REFERENCES `apps`(`id`) ON DELETE CASCADE);
CREATE UNIQUE INDEX `idx_nostr_events_nostr_id` ON `nostr_events`(`nostr_id`);
CREATE INDEX `idx_nostr_events_app_id` ON `nostr_events`(`app_id`);
CREATE INDEX idx_payment_sum ON payments (app_id, preimage, created_at);
CREATE INDEX idx_nostr_events_app_id_and_id ON nostr_events(app_id, id);
CREATE TABLE "response_events" (`id` integer,`app_id` integer null,`nostr_id` text UNIQUE,`request_id` integer,`content` text,`decrypted_content` text,`state` text,`replied_at` datetime,`created_at` datetime,`updated_at` datetime,PRIMARY KEY (`id`),CONSTRAINT `fk_response_events_app` FOREIGN KEY (`app_id`) REFERENCES `apps`(`id`) ON DELETE CASCADE,CONSTRAINT `fk_response_events_nostr_events` FOREIGN KEY (`request_id`) REFERENCES `nostr_events`(`id`));
CREATE UNIQUE INDEX `idx_response_events_nostr_id` ON `response_events`(`nostr_id`);
CREATE INDEX `idx_response_events_app_id` ON `response_events`(`app_id`);
CREATE TABLE "user_configs" ("id"	integer, "key"	text NOT NULL UNIQUE, "value"	text, "encrypted"	numeric, `created_at` datetime,`updated_at` datetime, PRIMARY KEY("id"));
CREATE UNIQUE INDEX "idx_user_configs_key" ON "user_configs" ("key");
