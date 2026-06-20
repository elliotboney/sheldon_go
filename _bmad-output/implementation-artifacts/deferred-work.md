# Deferred Work

## Deferred from: code review of 1-1-versioned-contracts-gob-round-trip (2026-06-20)

- **AllKinds mutability and Kind-AllKinds sync gap** — unsure which fix to take (unexported+Kinds() vs exported+comment); revisit when a second Kind is added.

- **gob type names include module path** — if module is forked/renamed, existing gob blobs produce "type not registered"; no test guards this. File: `contracts/register.go`.
- **Header.V is defined but nothing reads or gates on it** — intentional per architecture; version negotiation is future work. File: `contracts/envelope.go`.
- **No negative test for gob type-not-registered path** — future bus code should verify the error is catchable rather than a panic. File: `contracts/register.go`.
- **nil Payload in Envelope is untested** — gob behavior with nil interface field is unvalidated; relevant when bus enforces non-nil before encoding. File: `contracts/contracts_test.go`.

## Deferred from: code review of 1-2-core-owned-channel-hub-point-to-point-routing (2026-06-20)

- **Blocking `Publish` with no context/timeout** — intentional per spec Dev Notes; context/turn_id fencing deferred to Story 1.3. File: `core/bus/hub.go:57`.
- **No `Deregister` method** — routes are write-once; revisit when worker crashes require channel replacement. File: `core/bus/hub.go`.
- **`walkFields` missing Map/Interface/Chan descent** — spec acknowledges "structural today"; any future `contracts` type using map or interface fields is invisible to the NFR8 guard. File: `core/bus/hub_test.go:126`.
- **`Payload any` field not reflectively descended** — explicit `seeds` list is the current guard; new payload types must be added manually. File: `core/bus/hub_test.go:116`.
- **Send on closed channel panics in `Publish`** — consequence of absent `Deregister`; if a registrant closes its channel, `Publish` panics. Addressed when Deregister lands. File: `core/bus/hub.go:62`.
- **Hub observability absent** — no `Registered()`, `Len()`, or metrics hook; debugging routing failures requires external tooling. File: `core/bus/hub.go`.
- **Nil `Payload` forwarded without validation** — bus is dumb router; payload nil-checks belong at consumer. File: `core/bus/hub.go:57`.
- **Empty-string `Kind` accepted in `Register`** — zero-value `Envelope` would route to it; validate at ingress if this becomes a concern. File: `core/bus/hub.go:43`.

## Deferred from: code review of 1-3-worker-seam-interface-stub-1-in-flight-arbiter-gate (2026-06-20)

- **`Submit` has no `ctx.Done()` arm** — a context cancelled before slot acquisition returns `ErrTurnInFlight` instead of `ctx.Err()`; callers can't distinguish "slot busy" from "context dead." Related to AD-11 turn fencing; deferred to the turn lifecycle story. File: `core/arbiter/arbiter.go:37-43`.
