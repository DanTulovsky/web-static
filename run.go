package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/DanTulovsky/web-static/server"
	// go mod edit -replace github.com/lightstep/otel-launcher-go=/Users/dant/go/src/github.com/lightstep/otel-launcher-go
	"github.com/lightstep/otel-launcher-go/launcher"
	"go.opentelemetry.io/otel"

	_ "net/http/pprof"
)

const (
	// jaegerSamplingServerURL = "http://otel-collector.observability:5778/sampling"
	// jaegerCollectorEndpoint = "http://otel-collector.observability:14268/api/traces"
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
		// launcher.WithPropagators([]string{"b3", "tracecontext"}),
		launcher.WithPropagators([]string{"b3", "baggage", "tracecontext"}),
		launcher.WithMetricsEnabled(*enableMetrics),
	)
	return ls
}

// func enableTracer() (io.Closer, error) {
// 	log.Printf("Enabling OpenTracing tracer...")

// 	// ambassador uses zipkin
// 	zipkinPropagator := zipkin.NewZipkinB3HTTPHeaderPropagator()
// 	serviceName := jaegerServiceName
// 	jLogger := jaegerlog.StdLogger
// 	jMetricsFactory := metrics.NullFactory

// 	cfg, err := jaegercfg.FromEnv()
// 	if err != nil {
// 		// parsing errors might happen here, such as when we get a string where we expect a number
// 		log.Printf("Could not parse Jaeger env vars: %s", err.Error())
// 		return nil, err
// 	}

// 	cfg.Reporter.CollectorEndpoint = jaegerCollectorEndpoint
// 	// github.com/DanTulovsky/k8s-configs/configs/jaeger/operator-config.yaml has the config
// 	cfg.Sampler = &jaegercfg.SamplerConfig{
// 		Type:              jaeger.SamplerTypeRemote,
// 		Param:             0, // default sampling if server does not answer
// 		SamplingServerURL: jaegerSamplingServerURL,
// 		// Type:  jaeger.SamplerTypeConst,
// 		// Param: 1,
// 	}
// 	cfg.RPCMetrics = true

// 	// Create tracer and then initialize global tracer
// 	closer, err := cfg.InitGlobalTracer(
// 		serviceName,
// 		jaegercfg.Logger(jLogger),
// 		jaegercfg.Metrics(jMetricsFactory),
// 		// jaegercfg.Injector(opentracing.HTTPHeaders, zipkinPropagator),
// 		// upstream from ambassador is in zipkin format
// 		jaegercfg.Extractor(opentracing.HTTPHeaders, zipkinPropagator),
// 		jaegercfg.ZipkinSharedRPCSpan(true),
// 		// jaegercfg.Ta
// 	)

// 	if err != nil {
// 		log.Printf("Could not initialize jaeger tracer: %s", err.Error())
// 		return nil, err
// 	}

// 	return closer, nil
// }
