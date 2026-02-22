-- GetInstanceSettings fetches the single instance settings row
-- name: GetInstanceSettings :one
SELECT * FROM instance_settings WHERE id = 1;

-- UpsertRegistrationEnabled sets registration_enabled (creates row if missing)
-- name: UpsertRegistrationEnabled :exec
INSERT INTO instance_settings (id, registration_enabled, admin_emails, updated_at)
VALUES (1, sqlc.arg(registration_enabled), sqlc.arg(admin_emails), NOW())
ON CONFLICT (id) DO UPDATE
SET registration_enabled = EXCLUDED.registration_enabled,
    admin_emails = EXCLUDED.admin_emails,
    updated_at = NOW();

-- UpsertClipExportStorageLimit sets clip export storage limit (creates row if missing)
-- name: UpsertClipExportStorageLimit :exec
INSERT INTO instance_settings (id, registration_enabled, admin_emails, clip_export_storage_limit_bytes, updated_at)
VALUES (1, TRUE, ARRAY[]::text[], sqlc.arg(limit_bytes), NOW())
ON CONFLICT (id) DO UPDATE
SET clip_export_storage_limit_bytes = EXCLUDED.clip_export_storage_limit_bytes,
    updated_at = NOW();

-- UpsertAdminEmails sets admin emails (creates row if missing)
-- name: UpsertAdminEmails :exec
INSERT INTO instance_settings (id, registration_enabled, admin_emails, updated_at)
VALUES (1, TRUE, sqlc.arg(admin_emails), NOW())
ON CONFLICT (id) DO UPDATE
SET admin_emails = EXCLUDED.admin_emails,
    updated_at = NOW();
