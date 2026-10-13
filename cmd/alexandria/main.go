// Command alexandria runs the Library of Alexandria API and web server.
// CONFIG_FILE is the only environment variable it reads.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bmurray/alexandria/internal/app"
)

// version is set by the release build (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, version); err != nil {
		slog.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
