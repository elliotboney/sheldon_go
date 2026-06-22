// Package authtransport is the credential-injecting http.RoundTripper that turns
// the broker's raw model key into a pre-authorized client (AD-9). It is
// secret-touching code: living under broker/internal/ means Go's internal/ rule
// bars every package outside broker/ from importing it, so the key never leaves
// this boundary. The broker holds the key and hands it here; callers receive only
// the wrapped *http.Client and never see the secret.
package authtransport

import "net/http"

// Transport injects a bearer credential into every request before delegating to a
// base RoundTripper. The key is unexported and never logged.
type Transport struct {
	key  string
	base http.RoundTripper
}

// New returns a Transport that injects key as a bearer token. base is the
// underlying RoundTripper; when nil it falls back to http.DefaultTransport.
func New(key string, base http.RoundTripper) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{key: key, base: base}
}

// RoundTrip clones the request (the http.RoundTripper contract forbids mutating
// the caller's request), sets the Authorization header on the clone, and
// delegates to the base transport. When no credential is held (the degraded
// no-key broker), the header is omitted rather than sent as a malformed
// "Bearer " — the request goes out unauthenticated for the provider to reject.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.key != "" {
		clone.Header.Set("Authorization", "Bearer "+t.key)
	}
	return t.base.RoundTrip(clone)
}
