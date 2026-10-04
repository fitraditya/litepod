package logger

import (
	"context"
	"io"
	"os"

	obrellog "github.com/obrel/go-lib/pkg/log"
	"github.com/sirupsen/logrus"
)

// Logger and Fields alias the underlying logrus types so callers depend only
// on this package, not on github.com/sirupsen/logrus directly.
type (
	Logger = logrus.Logger
	Fields = logrus.Fields
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyRemoteIP
)

// WithRequestMeta attaches the HTTP request ID and caller IP to ctx, so
// business-logic log lines (usecase layer) can be correlated back to the
// request that triggered them — a minimal audit trail of who did what.
func WithRequestMeta(ctx context.Context, remoteIP, requestID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyRemoteIP, remoteIP)
	ctx = context.WithValue(ctx, ctxKeyRequestID, requestID)
	return ctx
}

// FromContext returns base enriched with request_id/remote_ip fields from
// ctx, if WithRequestMeta set them (e.g. ctx came from a background job, not
// an HTTP request). Always safe to call.
func FromContext(ctx context.Context, base *Logger) *logrus.Entry {
	fields := Fields{}
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok && v != "" {
		fields["request_id"] = v
	}
	if v, ok := ctx.Value(ctxKeyRemoteIP).(string); ok && v != "" {
		fields["remote_ip"] = v
	}
	return base.WithFields(fields)
}

// NewSilent returns a standalone logger instance (not the shared singleton)
// with output discarded — for use in tests.
func NewSilent() *Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	l.SetLevel(logrus.PanicLevel)
	return l
}

// nodeIDHook stamps every log entry with the node's ID, so log lines from
// multiple agent instances can be told apart once aggregated centrally.
type nodeIDHook struct {
	nodeID string
}

func (h nodeIDHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h nodeIDHook) Fire(e *logrus.Entry) error {
	e.Data["node_id"] = h.nodeID
	return nil
}

// New initializes the shared logger (github.com/obrel/go-lib/pkg/log). Format
// is controlled by LOG_FORMAT env var ("json" for production, text
// otherwise), level by LOG_LEVEL. If sentryDSN is non-empty, errors are also
// reported to Sentry (environment from SENTRY_ENVIRONMENT, default
// "production"; release from APP_VERSION, default "dev").
func New(nodeID string, sentryDSN string) *Logger {
	obrellog.SetOutput(os.Stdout)

	if nodeID != "" {
		obrellog.AddHook(nodeIDHook{nodeID: nodeID})
	}

	if os.Getenv("LOG_FORMAT") == "json" {
		obrellog.SetFormat(obrellog.JSONFormat)
	} else {
		obrellog.SetFormat(obrellog.TextFormat)
	}

	level, err := logrus.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		level = logrus.InfoLevel
	}
	obrellog.SetLevel(obrellog.Level(level))

	if sentryDSN != "" {
		environment := os.Getenv("SENTRY_ENVIRONMENT")
		if environment == "" {
			environment = "production"
		}
		version := os.Getenv("APP_VERSION")
		if version == "" {
			version = "dev"
		}
		if err := obrellog.AddSentryHook(sentryDSN, environment, version); err != nil {
			obrellog.Standard().WithError(err).Error("Failed to add Sentry hook")
		}
	}

	return obrellog.Standard()
}
