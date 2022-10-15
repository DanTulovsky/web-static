package main

import (
	"context"
	"flag"
	"log"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"time"

	"github.com/lightstep/otel-launcher-go/launcher"
	"go.opentelemetry.io/otel"

	"github.com/DanTulovsky/web-static/server"
)

const (
	otelServiceName = "web-static"
)

var (
	gracefulTimeout = flag.Duration("graceful_timeout_sec", 5*time.Second, "duration to wait before shutting down")
	enableMetrics   = flag.Bool("enable_metrics", true, "Set to enable metrics via lightstep (requires tracing is enabled).")
	version         = flag.String("version", "", "version")
)

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting version: %v", *version)

	ls := enableOpenTelemetry()
	tracer := otel.Tracer("global")
	defer ls.Shutdown()

	feServer, err := server.NewServer(tracer)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := feServer.Run()
		if err != nil {
			return
		}
	}()

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	log.Printf("shutting down in %v if active connections...", *gracefulTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), *gracefulTimeout)
	defer cancel()
	err = feServer.Srv.Shutdown(ctx)
	if err != nil {
		return
	}
	os.Exit(0)
}

func enableOpenTelemetry() launcher.Launcher {
	log.Println("Enabling OpenTelemetry support...")
	// https://github.com/lightstep/otel-launcher-go
	ls := launcher.ConfigureOpentelemetry(
		launcher.WithServiceName(otelServiceName),
		launcher.WithServiceVersion(*version),
		// launcher.WithAccessToken("{your_access_token}"),  # in env
		launcher.WithLogLevel("info"),
		launcher.WithPropagators([]string{"b3", "baggage", "tracecontext"}),
		launcher.WithMetricsEnabled(*enableMetrics),
	)
	return ls
}
