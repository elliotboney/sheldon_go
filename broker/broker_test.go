package broker_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/elliotboney/shelldon_go/broker"
)

// TestClient_InjectsResolvedCredential is AC1: New() resolves the credential from
// the environment and Client() returns a pre-authorized client that injects it —
// callers send a plain request and the broker's transport adds the auth.
func TestClient_InjectsResolvedCredential(t *testing.T) {
	t.Setenv("SHELLDON_LLM_API_KEY", "sk-from-env")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	resp, err := broker.New().Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("request through broker client: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if want := "Bearer sk-from-env"; gotAuth != want {
		t.Fatalf("broker client injected %q, want %q", gotAuth, want)
	}
}

// TestNew_MissingCredentialDoesNotPanic is AC1's degraded path: with no env
// credential the broker still constructs (logs the absence; the worker/provider
// chain decides fallback) rather than panicking.
func TestNew_MissingCredentialDoesNotPanic(t *testing.T) {
	t.Setenv("SHELLDON_LLM_API_KEY", "")

	b := broker.New()
	if b.Client() == nil {
		t.Fatal("broker with no credential returned a nil client")
	}
}

// TestBroker_ExposesNoRawKeyAccessor is AC1 (NFR8): the broker's exported API must
// be Client() only — no exported field or method may surface the raw key. This
// fails if a future change adds a credential-shaped exported member.
func TestBroker_ExposesNoRawKeyAccessor(t *testing.T) {
	// The public surface is asserted by reflection over exported methods and the
	// (zero) exported fields of *Broker.
	bt := reflect.TypeOf(broker.New())

	for i := 0; i < bt.NumMethod(); i++ {
		name := strings.ToLower(bt.Method(i).Name)
		for _, bad := range []string{"key", "secret", "token", "credential", "auth"} {
			if strings.Contains(name, bad) {
				t.Errorf("Broker exposes method %q — must not surface the raw credential (NFR8)", bt.Method(i).Name)
			}
		}
	}

	// Broker holds only unexported fields; any exported field is a leak risk.
	elem := bt.Elem()
	for i := 0; i < elem.NumField(); i++ {
		if f := elem.Field(i); f.IsExported() {
			t.Errorf("Broker has exported field %q — credential machinery must stay unexported (NFR8)", f.Name)
		}
	}
}
