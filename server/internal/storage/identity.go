package storage

// Shared relational roots for the single application database.
const IdentitySchema = `
CREATE TABLE IF NOT EXISTS app_users (
 id TEXT PRIMARY KEY, display_name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled','merged')),
 created_at TEXT NOT NULL, merged_into TEXT REFERENCES app_users(id)
);
CREATE TABLE IF NOT EXISTS user_access(
 user_id TEXT PRIMARY KEY REFERENCES app_users(id), enabled INTEGER NOT NULL DEFAULT 0,
 role TEXT NOT NULL DEFAULT 'member' CHECK(role IN ('member','superadmin')), version INTEGER NOT NULL DEFAULT 1
);
`
