package obs

import "expvar"

// AuditDrops is a package-level expvar counter for audit channel overflows.
// Increment this when the audit event channel is full and an event is dropped.
var AuditDrops = expvar.NewInt("audit_drops")
