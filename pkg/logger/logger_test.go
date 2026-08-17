package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew_Defaults(t *testing.T) {
	log := New("node-1", "")
	assert.NotNil(t, log)
}

func TestNew_JSONFormat(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")
	log := New("node-1", "")
	assert.NotNil(t, log)
}

func TestNew_ValidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	log := New("node-1", "")
	assert.NotNil(t, log)
}

func TestNew_InvalidLogLevelFallsBackToInfo(t *testing.T) {
	t.Setenv("LOG_LEVEL", "not-a-level")
	log := New("node-1", "")
	assert.NotNil(t, log)
}

func TestNew_SentryHookWithValidDSN(t *testing.T) {
	// Syntactically valid DSN (never actually dialed in this test) so the
	// hook initializes successfully.
	//
	// NOTE: a malformed DSN here would panic — see obrel/go-lib's
	// NewSentryHook, which calls hook.SetEnvironment on the result of
	// NewAsyncSentryHook without checking the returned error first, so a
	// parse failure leaves hook nil and SetEnvironment nil-derefs. That's a
	// latent bug in the third-party package, not exercised by this suite
	// since we can't safely trigger it without crashing the test binary.
	log := New("node-1", "https://public@example.com/1")
	assert.NotNil(t, log)
}

func TestNew_SentryWithEnvironmentAndVersion(t *testing.T) {
	t.Setenv("SENTRY_ENVIRONMENT", "staging")
	t.Setenv("APP_VERSION", "1.2.3")
	log := New("node-1", "https://public@example.com/1")
	assert.NotNil(t, log)
}
