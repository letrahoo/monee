CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE ledgers (
 id TEXT PRIMARY KEY, currency TEXT NOT NULL CHECK(currency='CNY'), timezone TEXT NOT NULL,
 version INTEGER NOT NULL, device_id TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE accounts (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), name TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('clearing','expense','income')), version INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
 UNIQUE(ledger_id,kind,name)
);
CREATE TABLE transactions (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), occurred_on TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('expense','income')), amount_minor INTEGER NOT NULL,
 currency TEXT NOT NULL CHECK(currency='CNY'), merchant TEXT NOT NULL, category TEXT NOT NULL,
 source TEXT NOT NULL, account TEXT NOT NULL, external_id TEXT NOT NULL, note TEXT NOT NULL,
 identity_key TEXT, fingerprint TEXT NOT NULL, similarity_key TEXT NOT NULL,
 version INTEGER NOT NULL, device_id TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
 CHECK((kind='expense' AND amount_minor<0) OR (kind='income' AND amount_minor>0)),
 UNIQUE(ledger_id,identity_key)
);
CREATE INDEX transactions_month ON transactions(ledger_id,occurred_on DESC,id);
CREATE INDEX transactions_similarity ON transactions(ledger_id,similarity_key);
CREATE TABLE postings (
 id TEXT PRIMARY KEY, transaction_id TEXT NOT NULL REFERENCES transactions(id),
 account_id TEXT NOT NULL REFERENCES accounts(id), amount_minor INTEGER NOT NULL, currency TEXT NOT NULL CHECK(currency='CNY')
);
CREATE TABLE imports (
 id TEXT PRIMARY KEY, ledger_id TEXT NOT NULL REFERENCES ledgers(id), filename TEXT NOT NULL,
 content_hash TEXT NOT NULL, raw_csv TEXT NOT NULL, parser_version INTEGER NOT NULL,
 ledger_version INTEGER NOT NULL, preview_json TEXT NOT NULL, result_json TEXT,
 created_at TEXT NOT NULL, committed_at TEXT,
 UNIQUE(ledger_id,content_hash)
);
CREATE TABLE source_records (
 id TEXT PRIMARY KEY, import_id TEXT NOT NULL REFERENCES imports(id), line INTEGER NOT NULL,
 transaction_id TEXT NOT NULL REFERENCES transactions(id), normalized_json TEXT NOT NULL,
 disposition TEXT NOT NULL CHECK(disposition IN ('added','duplicate')), UNIQUE(import_id,line)
);
CREATE TABLE idempotency (
 key TEXT PRIMARY KEY, request_hash TEXT NOT NULL, response_json TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE change_log (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, change_id TEXT NOT NULL UNIQUE,
 ledger_id TEXT NOT NULL REFERENCES ledgers(id), device_id TEXT NOT NULL,
 entity_id TEXT NOT NULL, entity_type TEXT NOT NULL, base_version INTEGER NOT NULL,
 entity_version INTEGER NOT NULL, operation TEXT NOT NULL, payload_version INTEGER NOT NULL,
 payload_json TEXT NOT NULL, created_at TEXT NOT NULL
);
PRAGMA user_version=1;
