package contracts

// MemoryOpCaptureLearning is the FR11 hot-path op kind: the worker proposes a
// single observation (with an optional dedup pattern key) for core to record.
const MemoryOpCaptureLearning = "capture_learning"

// MemoryOp is a memory mutation the worker proposes. The worker never writes
// directly; this is the proposal channel — the worker proposes, core applies
// (AD-6). Each op kind uses the subset of fields its schema defines:
// capture_learning uses Observation plus an optional PatternKey; other kinds add
// their own fields later.
type MemoryOp struct {
	Kind        string // proposed op kind, e.g. MemoryOpCaptureLearning
	Observation string // capture_learning: the observed text to record
	PatternKey  string // capture_learning: optional dedup key; "" never dedups
}

// Result is what the worker proposes back to core: the reply text plus any
// proposed memory operations. The worker proposes; core decides and writes.
type Result struct {
	Reply     string     // proposed reply text
	MemoryOps []MemoryOp // proposed memory mutations (never applied by the worker)
}

func (Result) isPayload() {}
