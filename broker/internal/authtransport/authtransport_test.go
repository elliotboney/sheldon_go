package authtransport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRoundTrip_InjectsBearer proves the transport sets the Authorization header
// on the outbound request (the auth-injection AD-9 idiom).
func TestRoundTrip_InjectsBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	client := &http.Client{Transport: New("sk-secret", nil)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if want := "Bearer sk-secret"; gotAuth != want {
		t.Fatalf("injected Authorization = %q, want %q", gotAuth, want)
	}
}

// TestRoundTrip_OmitsHeaderWhenNoKey proves the degraded no-credential broker
// sends no Authorization header at all, rather than a malformed "Bearer ".
func TestRoundTrip_OmitsHeaderWhenNoKey(t *testing.T) {
	hasAuth := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
	}))
	defer srv.Close()

	client := &http.Client{Transport: New("", nil)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if hasAuth {
		t.Fatal("empty key sent an Authorization header, want none")
	}
}

// TestRoundTrip_DoesNotMutateCallerRequest guards the http.RoundTripper contract:
// the caller's request must be untouched — the header goes on a clone only.
func TestRoundTrip_DoesNotMutateCallerRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := New("sk-secret", nil).RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("caller request was mutated: Authorization = %q, want empty", got)
	}
}
