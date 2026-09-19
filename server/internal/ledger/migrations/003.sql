-- Executed with foreign_keys disabled on a dedicated connection, inside one
-- transaction. Copy, replace, then foreign_key_check before committing.
CREATE TABLE transactions_v3 (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), occurred_on TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('expense','income','refund')), amount_minor INTEGER NOT NULL,
 currency TEXT NOT NULL CHECK(currency='CNY'), merchant TEXT NOT NULL, category TEXT NOT NULL,
 source TEXT NOT NULL, account TEXT NOT NULL, external_id TEXT NOT NULL, note TEXT NOT NULL,
 identity_key TEXT, fingerprint TEXT NOT NULL, similarity_key TEXT NOT NULL,
 version INTEGER NOT NULL, device_id TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
 created_by TEXT REFERENCES app_users(id), refund_of TEXT,
 CHECK((kind='expense' AND amount_minor<0) OR (kind IN ('income','refund') AND amount_minor>0)),
 CHECK((kind='refund' AND refund_of IS NOT NULL AND refund_of<>id) OR (kind<>'refund' AND refund_of IS NULL)),
 UNIQUE(ledger_id,identity_key), UNIQUE(ledger_id,id),
 FOREIGN KEY(ledger_id,refund_of) REFERENCES transactions_v3(ledger_id,id)
);
INSERT INTO transactions_v3 (
 id,ledger_id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,
 identity_key,fingerprint,similarity_key,version,device_id,created_at,updated_at,deleted_at,created_by
) SELECT
 id,ledger_id,occurred_on,kind,amount_minor,currency,merchant,category,source,account,external_id,note,
 identity_key,fingerprint,similarity_key,version,device_id,created_at,updated_at,deleted_at,created_by
FROM transactions;
DROP TABLE transactions;
ALTER TABLE transactions_v3 RENAME TO transactions;
CREATE INDEX transactions_month ON transactions(ledger_id,occurred_on DESC,id);
CREATE INDEX transactions_similarity ON transactions(ledger_id,similarity_key);
CREATE INDEX transactions_refund ON transactions(ledger_id,refund_of) WHERE refund_of IS NOT NULL;
PRAGMA user_version=3;
