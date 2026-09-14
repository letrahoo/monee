ALTER TABLE ledgers ADD COLUMN name TEXT NOT NULL DEFAULT '原有账本';
CREATE TABLE ledger_members (
 ledger_id TEXT NOT NULL REFERENCES ledgers(id), user_id TEXT NOT NULL REFERENCES app_users(id),
 role TEXT NOT NULL CHECK(role IN ('owner','editor','viewer')), version INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, PRIMARY KEY(ledger_id,user_id)
);
CREATE UNIQUE INDEX one_ledger_owner ON ledger_members(ledger_id) WHERE role='owner';
CREATE TABLE ledger_invitations (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), user_id TEXT NOT NULL REFERENCES app_users(id),
 role TEXT NOT NULL CHECK(role IN ('editor','viewer')), invited_by TEXT NOT NULL REFERENCES app_users(id),
 status TEXT NOT NULL CHECK(status IN ('pending','accepted','declined')), created_at TEXT NOT NULL,
 UNIQUE(ledger_id,user_id,status)
);
CREATE TABLE ledger_audit (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), actor_id TEXT NOT NULL REFERENCES app_users(id),
 action TEXT NOT NULL, target_id TEXT NOT NULL, created_at TEXT NOT NULL
);
ALTER TABLE transactions ADD COLUMN created_by TEXT REFERENCES app_users(id);
ALTER TABLE imports ADD COLUMN created_by TEXT REFERENCES app_users(id);
ALTER TABLE change_log ADD COLUMN actor_id TEXT REFERENCES app_users(id);
ALTER TABLE idempotency RENAME TO idempotency_v1;
CREATE TABLE idempotency (
 ledger_id TEXT NOT NULL REFERENCES ledgers(id), key TEXT NOT NULL, request_hash TEXT NOT NULL,
 response_json TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(ledger_id,key)
);
INSERT INTO idempotency SELECT t.ledger_id,i.key,i.request_hash,i.response_json,i.created_at
FROM idempotency_v1 i JOIN transactions t ON t.id=json_extract(i.response_json,'$.id');
DROP TABLE idempotency_v1;
PRAGMA user_version=2;
