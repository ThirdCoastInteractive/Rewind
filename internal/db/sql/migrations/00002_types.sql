-- +goose Up
-- Enable pgcrypto for encryption functions
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Domains for type safety
CREATE DOMAIN encrypted_string AS bytea;
CREATE DOMAIN encrypted_bytes AS bytea;
CREATE DOMAIN markdown_src AS TEXT;
CREATE DOMAIN language_tag AS TEXT;
CREATE DOMAIN hashed_password AS TEXT;
CREATE DOMAIN asset_status_map AS JSONB;
CREATE DOMAIN hex_color AS TEXT CHECK (VALUE ~ '^#[0-9a-fA-F]{6}$');

-- ENUM types for constrained values
CREATE TYPE job_status AS ENUM ('queued', 'processing', 'succeeded', 'failed');
CREATE TYPE marker_type AS ENUM ('point', 'chapter');
CREATE TYPE user_role AS ENUM ('user', 'admin');
CREATE TYPE export_status AS ENUM ('processing', 'ready', 'error');
CREATE TYPE log_stream AS ENUM ('stdout', 'stderr');
CREATE TYPE export_variant AS ENUM ('full', 'cropped');

-- +goose Down
DROP TYPE IF EXISTS export_variant;
DROP TYPE IF EXISTS log_stream;
DROP TYPE IF EXISTS export_status;
DROP TYPE IF EXISTS user_role;
DROP TYPE IF EXISTS marker_type;
DROP TYPE IF EXISTS job_status;
DROP DOMAIN IF EXISTS hex_color;
DROP DOMAIN IF EXISTS asset_status_map;
DROP DOMAIN IF EXISTS hashed_password;
DROP DOMAIN IF EXISTS language_tag;
DROP DOMAIN IF EXISTS markdown_src;
DROP DOMAIN IF EXISTS encrypted_bytes;
DROP DOMAIN IF EXISTS encrypted_string;
DROP EXTENSION IF EXISTS pgcrypto;
