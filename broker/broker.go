// Package broker is the sole trust boundary (AD-9): the only holder of model/tool
// credentials and the only egress to models. Credentials resolve only here, from
// the environment — never from the bus, never in source. The broker exposes only
// a pre-authorized *http.Client (auth injected by broker/internal/authtransport);
// downstream callers (the worker, Story 3.3) use that client and never see the
// raw key. No credential ever rides the bus (NFR8): Job/Result carry none, the
// broker injects them internally at egress.
//
// The ordered provider chain (failsafe-go, GLM default via base-URL swap) lands
// behind this boundary in Story 3.2; provider SDKs live only under
// broker/internal/, enforced by depguard (.golangci.yml) and Go's internal/ rule.
package broker

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/elliotboney/shelldon_go/broker/internal/authtransport"
)

// apiKeyEnv is the environment variable the model credential resolves from.
// Story-time config (the concrete provider/base-URL selection is Story 3.2), not
// a spine invariant.
const apiKeyEnv = "SHELLDON_LLM_API_KEY"

// Broker holds the resolved model credential (unexported, never logged) and the
// pre-authorized client that injects it.
type Broker struct {
	client *http.Client
}

// New resolves the model credential from the environment and builds the
// pre-authorized client. A missing credential is not fatal: New returns a broker
// whose client carries an empty bearer, and logs the absence (AD-17) so the
// provider chain (3.2) and worker (3.3) can decide fallback — the pet degrades to
// reflex rather than crashing. The key value is never logged.
func New() *Broker {
	key := os.Getenv(apiKeyEnv)
	if key == "" {
		slog.Warn("broker: no model credential resolved; LLM egress unavailable", "env", apiKeyEnv)
	} else {
		slog.Info("broker: model credential resolved", "env", apiKeyEnv)
	}
	return &Broker{
		client: &http.Client{Transport: authtransport.New(key, nil)},
	}
}

// Client returns the pre-authorized HTTP client — the broker's only exported
// access to its credential machinery. The client injects auth on every request;
// there is no exported path to the raw key (NFR8/AD-9).
func (b *Broker) Client() *http.Client {
	return b.client
}
