package bus

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/elliotboney/shelldon_go/contracts"
)

func jobEnvelope(input string) contracts.Envelope {
	return contracts.Envelope{
		Header:  contracts.Header{ID: "e1", V: 1, Kind: contracts.KindJob, Src: "core", Dst: "worker", TurnID: "t1"},
		Payload: contracts.Job{Input: input, ConvoID: "c1"},
	}
}

// TestRegister_DuplicateRoute proves a kind can be registered once and a second
// registration fails safe with ErrDuplicateRoute, leaving the original route intact.
func TestRegister_DuplicateRoute(t *testing.T) {
	h := New()
	first := make(chan contracts.Envelope, 1)
	if err := h.Register(contracts.KindJob, first); err != nil {
		t.Fatalf("first Register: unexpected error %v", err)
	}

	second := make(chan contracts.Envelope, 1)
	if err := h.Register(contracts.KindJob, second); !errors.Is(err, ErrDuplicateRoute) {
		t.Fatalf("second Register: got %v, want ErrDuplicateRoute", err)
	}

	// The original route must still be the one that receives.
	if err := h.Publish(jobEnvelope("hi")); err != nil {
		t.Fatalf("Publish: unexpected error %v", err)
	}
	select {
	case <-first:
	default:
		t.Error("original destination did not receive; duplicate Register clobbered the route")
	}
	if len(second) != 0 {
		t.Error("second destination received; duplicate registration must not take effect")
	}
}

// TestRegister_NilDestination proves a nil destination channel is rejected at
// registration rather than stored to hang the first Publish (review patch).
func TestRegister_NilDestination(t *testing.T) {
	h := New()
	if err := h.Register(contracts.KindJob, nil); !errors.Is(err, ErrNilDestination) {
		t.Fatalf("got %v, want ErrNilDestination", err)
	}
	// The kind must remain unregistered, so a later real registration succeeds.
	if err := h.Register(contracts.KindJob, make(chan contracts.Envelope, 1)); err != nil {
		t.Fatalf("kind should be free after nil rejection, got %v", err)
	}
}

// TestPublish_PointToPoint proves an envelope is delivered to exactly the
// destination registered for its kind and to no other (AC1).
func TestPublish_PointToPoint(t *testing.T) {
	h := New()
	jobCh := make(chan contracts.Envelope, 1)
	resultCh := make(chan contracts.Envelope, 1)
	if err := h.Register(contracts.KindJob, jobCh); err != nil {
		t.Fatalf("Register job: %v", err)
	}
	if err := h.Register(contracts.KindResult, resultCh); err != nil {
		t.Fatalf("Register result: %v", err)
	}

	want := jobEnvelope("hello")
	if err := h.Publish(want); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-jobCh:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("delivered envelope mismatch\n got: %#v\nwant: %#v", got, want)
		}
	default:
		t.Fatal("KindJob destination received nothing")
	}
	if len(resultCh) != 0 {
		t.Error("KindResult destination received an envelope addressed to KindJob")
	}
}

// TestPublish_NoRoute proves publishing to an unregistered kind returns
// ErrNoRoute and never panics (AC2).
func TestPublish_NoRoute(t *testing.T) {
	h := New()
	// Only KindResult is registered; the Job envelope has nowhere to go.
	if err := h.Register(contracts.KindResult, make(chan contracts.Envelope, 1)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Publish panicked on unknown route: %v", r)
			}
		}()
		return h.Publish(jobEnvelope("orphan"))
	}()

	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("got %v, want ErrNoRoute", err)
	}
	// The error must name the offending kind for diagnostics (review patch).
	if !strings.Contains(err.Error(), string(contracts.KindJob)) {
		t.Errorf("error %q does not name the offending kind %q", err, contracts.KindJob)
	}
}

// credentialPattern matches field names that would indicate a credential on the
// bus. Bare "key"/"auth" are intentionally excluded — they false-positive on
// legitimate non-secret fields (e.g. turn_id, author) — while real credential
// shapes (api keys, bearer tokens, secrets, passwords) are caught.
var credentialPatterns = []string{
	"token", "secret", "password", "passwd", "credential",
	"apikey", "api_key", "accesskey", "access_key",
	"privatekey", "private_key", "bearer",
}

// TestEnvelopeCarriesNoCredentials walks the full field graph of every type that
// can traverse the hub and fails if any field name looks like a credential
// (NFR8, AC3). The guarantee is structural today; this guards it as payloads grow.
func TestEnvelopeCarriesNoCredentials(t *testing.T) {
	seeds := []reflect.Type{
		reflect.TypeOf(contracts.Envelope{}),
		reflect.TypeOf(contracts.Job{}),
		reflect.TypeOf(contracts.Result{}),
	}
	for _, seed := range seeds {
		walkFields(t, seed, seed.Name())
	}
}

func walkFields(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		walkFields(t, typ.Elem(), path+"[]")
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			lower := strings.ToLower(f.Name)
			for _, pat := range credentialPatterns {
				if strings.Contains(lower, pat) {
					t.Errorf("credential-like field on the bus: %s.%s (matched %q) — NFR8 forbids creds on the bus", path, f.Name, pat)
				}
			}
			walkFields(t, f.Type, path+"."+f.Name)
		}
	}
}

// TestHub_ConcurrentRaceClean exercises concurrent Register + Publish (registered
// and unregistered kinds) so `go test -race` proves the routing table is safe.
func TestHub_ConcurrentRaceClean(t *testing.T) {
	h := New()
	jobCh := make(chan contracts.Envelope, 64)
	if err := h.Register(contracts.KindJob, jobCh); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Drain so blocking sends complete.
	go func() {
		for range jobCh {
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Publish(jobEnvelope("x"))                                                         // registered → delivered
			_ = h.Publish(contracts.Envelope{Header: contracts.Header{Kind: contracts.KindResult}}) // unregistered → ErrNoRoute
		}()
	}
	wg.Wait()
	close(jobCh)
}
