package storage

// SchemaVersion is the default and automatic ledger migration target.
// Schema 6 requires an explicit offline upgrade; opening a schema 5 DB must not
// opt it into expense-pair decisions.
const SchemaVersion = 5

// MaxSupportedSchemaVersion is the startup and restore compatibility ceiling.
// Identity initialization must preserve the containing application's version.
const MaxSupportedSchemaVersion = 6
