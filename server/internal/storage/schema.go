package storage

// SchemaVersion is shared by the ledger, identity store and health endpoint.
// Identity initialization must never downgrade the containing application DB.
const SchemaVersion = 3
