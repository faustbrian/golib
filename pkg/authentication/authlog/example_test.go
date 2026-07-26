package authlog_test

import (
	"context"
	"log/slog"
	"os"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authlog"
)

func ExampleNew() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if attribute.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attribute
		},
	})
	instrumenter, _ := authlog.New(slog.New(handler))
	_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated, Duration: time.Millisecond})
	// Output: {"level":"INFO","msg":"authentication completed","credential_kind":"bearer","outcome":"authenticated","failure_kind":"","duration_ms":1}
}
