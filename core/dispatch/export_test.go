package dispatch

// ReflexAckForTest exposes the unexported reflexAck to the external dispatch_test
// package so tests assert the exact canned value — if reflexAck changes, the
// tests track it instead of silently passing on a stale literal.
const ReflexAckForTest = reflexAck
