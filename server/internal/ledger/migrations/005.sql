-- Storage foundation only. No decision writer or effective financial projection
-- is activated by this migration. This binary refuses to open nonempty tables.
CREATE TABLE expense_pair_decisions (
 ledger_id TEXT NOT NULL REFERENCES ledgers(id),
 id TEXT NOT NULL CHECK(typeof(id)='text' AND length(trim(id))>0),
 status TEXT NOT NULL CHECK(status IN ('active','retracted')),
 version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
 contract_version INTEGER NOT NULL CHECK(typeof(contract_version)='integer' AND contract_version=1),
 created_by TEXT NOT NULL REFERENCES app_users(id),
 created_at TEXT NOT NULL CHECK(typeof(created_at)='text' AND length(created_at)>0),
 updated_at TEXT NOT NULL CHECK(typeof(updated_at)='text' AND length(updated_at)>0),
 PRIMARY KEY(ledger_id,id)
);
CREATE TABLE expense_pair_members (
 ledger_id TEXT NOT NULL,
 decision_id TEXT NOT NULL,
 transaction_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('primary','secondary')),
 transaction_version INTEGER NOT NULL CHECK(typeof(transaction_version)='integer' AND transaction_version>0),
 facts_json TEXT NOT NULL CHECK(typeof(facts_json)='text' AND json_valid(facts_json) AND json_type(facts_json)='object'),
 evidence_json TEXT NOT NULL CHECK(typeof(evidence_json)='text' AND json_valid(evidence_json) AND json_type(evidence_json)='object'),
 active INTEGER NOT NULL CHECK(typeof(active)='integer' AND active IN (0,1)),
 PRIMARY KEY(ledger_id,decision_id,transaction_id),
 UNIQUE(ledger_id,decision_id,role),
 FOREIGN KEY(ledger_id,decision_id) REFERENCES expense_pair_decisions(ledger_id,id),
 FOREIGN KEY(ledger_id,transaction_id) REFERENCES transactions(ledger_id,id)
);
CREATE UNIQUE INDEX expense_pair_members_one_active_transaction ON expense_pair_members(ledger_id,transaction_id) WHERE active=1;
CREATE TABLE expense_pair_revisions (
 ledger_id TEXT NOT NULL,
 decision_id TEXT NOT NULL,
 revision_id TEXT NOT NULL CHECK(typeof(revision_id)='text' AND length(trim(revision_id))>0),
 version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
 operation TEXT NOT NULL CHECK(operation IN ('confirm','withdraw')),
 actor_id TEXT NOT NULL REFERENCES app_users(id),
 reason TEXT NOT NULL CHECK(typeof(reason)='text' AND length(trim(reason))>0),
 snapshot_json TEXT NOT NULL CHECK(typeof(snapshot_json)='text' AND json_valid(snapshot_json) AND json_type(snapshot_json)='object'),
 contract_version INTEGER NOT NULL CHECK(typeof(contract_version)='integer' AND contract_version=1),
 created_at TEXT NOT NULL CHECK(typeof(created_at)='text' AND length(created_at)>0),
 PRIMARY KEY(ledger_id,decision_id,revision_id),
 UNIQUE(ledger_id,decision_id,version),
 FOREIGN KEY(ledger_id,decision_id) REFERENCES expense_pair_decisions(ledger_id,id)
);
PRAGMA user_version=5;
