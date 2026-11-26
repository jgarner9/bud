package mcpgateway

import "time"

// Record is what the gateway knows about one call, which it writes to the
// shared audit log as a tool_call event (Suite D140). Argument values are
// left out, since they can hold private text or keys; their names say what
// was asked
type Record struct {
	Time   time.Time
	Server string
	Tier   string
	// Raised names the privacy rule that raised Tier, if one did
	Raised   string
	Client   string
	Method   string
	Tool     string
	Args     []string
	Decision string
	Status   int
	Millis   int64
}
