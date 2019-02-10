package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"
	"go.opencensus.io/stats/view"
	"gogs.wetsnow.com/dant/web-static/server"
)

var (
	addr            = flag.String("listen-address", ":3000", "The address to listen on for HTTP requests.")
	gracefulTimeout = flag.Duration("graceful_timeout_sec", 5*time.Second, "duration to wait before shutting down")
	enableMetrics   = flag.Bool("enable_metrics", false, "Set to enable metrics via stackdriver.")
)

func enableStackdriverIntegration() *stackdriver.Exporter {
	// enable OpenCensus views (prometheus, stackdriver integration)
	server.EnableViews()
	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:         "snowcloud-01",
		MonitoredResource: monitoredresource.Autodetect(),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Register so that views are exported.
	view.RegisterExporter(exporter)
	view.SetReportingPeriod(60 * time.Second)

	return exporter
}

func main() {
	flag.Parse()

	if *enableMetrics {
		exporter := enableStackdriverIntegration()
		defer exporter.Flush()
	}

	feServer, err := server.NewServer(*addr)
	if err != nil {
		log.Fatal(err)
	}
	feServer.RegisterHandlers()

	go func() {
		log.Printf("Staritng http server on %v", *addr)
		if err := feServer.Srv.ListenAndServe(); err != nil {
			log.Fatal(err)
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
	feServer.Srv.Shutdown(ctx)
	os.Exit(0)
}
