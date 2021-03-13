package server

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/enriquebris/goconcurrentqueue"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
	. "go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"
)

// gcr.io/snowcloud-01/static-web/frontend:YYYYMMDD00

const (
	kafkaQueueMaxSize = 10
)

var (
	enableLogs  = flag.Bool("enable_logging", true, "Set to enable logging.")
	enableKafka = flag.Bool("enable_kafka", false, "Set to true to enable kafka support")
	logDir      = flag.String("log_dir", "", "Top level directory for log files, if empty (and enable_logging) logs to stdout")
	dataDir     = flag.String("data_dir", "data/hosts", "Top level directory for site files.")
	pprofPort   = flag.String("pprof_port", "6060", "port for pprof")
	addr        = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
)

// RootHandler handles requests
type RootHandler struct {
}

type debugLogger struct{}

func (d debugLogger) Write(p []byte) (n int, err error) {
	s := string(p)
	if strings.Contains(s, "multiple response.WriteHeader") {
		debug.PrintStack()
	}
	return os.Stderr.Write(p)
}

// Server is the frontend server
type Server struct {
	Srv    *http.Server
	tracer trace.Tracer
}

// NewServer returns a server
func NewServer(tracer trace.Tracer) (*Server, error) {
	// Now use the logger with your http.Server:
	logger := log.New(debugLogger{}, "", 0)

	return &Server{
		Srv: &http.Server{
			Addr:         *addr,
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
			IdleTimeout:  time.Second * 60,
			ErrorLog:     logger,
		},
		tracer: tracer,
	}, nil
}

func enableLogging() io.Writer {
	var logFile = ioutil.Discard

	if !*enableLogs {
		log.Print("logging disabled...")
		return logFile
	}

	if *logDir == "" {
		log.Print("logging to stdout...")
		logFile = os.Stdout
	} else {
		file := fmt.Sprintf("%s/access_log", *logDir)
		log.Printf("logging to %v", logFile)

		var err error
		logFile, err = os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
	}
	return logFile
}

// Run runs the server
func (s *Server) Run() error {

	if *enableKafka {
		kafkaQueue := goconcurrentqueue.NewFixedFIFO(kafkaQueueMaxSize)
		s.RegisterHandlers(kafkaQueue)
		go s.kafkaSubscribe(kafkaQueue)
	}

	go s.startPprof()

	log.Printf("Staritng http server on %v", *addr)
	return s.Srv.ListenAndServe()
}

func (s *Server) kafkaSubscribe(kafkaQueue goconcurrentqueue.Queue) {
	log.Println("Starting kafka consumer...")
	c := newKafkaConsumer()
	defer c.Close()

	for {
		msg, err := c.ReadMessage(-1)
		if err == nil {
			// add to queue
			if err := kafkaQueue.Enqueue(string(msg.Value)); err != nil {
				// log.Printf("queue error: %v", err)
			}
			// fmt.Printf("Message on %s: %s\n", msg.TopicPartition, string(msg.Value))
		} else {
			fmt.Printf("err talking to kafka: %v", err)
		}
	}
}

func (s *Server) startPprof() error {
	log.Printf("Starting pprof on port %v", *pprofPort)
	pprofMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()

	log.Println(http.ListenAndServe(fmt.Sprintf("localhost:%s", *pprofPort), pprofMux))

	return nil
}

// RegisterHandlers registers http handlers
func (s *Server) RegisterHandlers(kafkaQueue goconcurrentqueue.Queue) {
	logFile := enableLogging()

	r := mux.NewRouter()
	s.Srv.Handler = r

	// metrics (todo: replace with open-telemetry)
	mdlw := middleware.New(middleware.Config{
		Recorder:      metrics.NewRecorder(metrics.Config{}),
		Service:       "web-static",
		GroupedStatus: true,
	})

	// opentracing
	tHandler := &tracingHandler{
		tracer: s.tracer,
	}

	r.Use(std.HandlerProvider("default", mdlw))
	r.Use(tHandler.Middleware)

	// Prometheus metrics
	r.Handle("/metrics", std.Handler("/metrics", mdlw,
		handlers.CombinedLoggingHandler(logFile, promhttp.Handler())))

	// Health Checks used by kubernetes
	r.HandleFunc("/healthz", HandleHealthz)
	r.HandleFunc("/servez", HandleServez)
	r.HandleFunc("/env", HandleEnv)

	// wetsnow.com redirect
	r.Host("wetsnow.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.wetsnow.com/", http.StatusMovedPermanently)
	})

	// wetsnow.com/kafka
	kfkHandler := std.Handler("wetsnow.com", mdlw, handlers.CombinedLoggingHandler(logFile, newKafkaHandler(kafkaQueue)))
	r.Host("www.wetsnow.com").PathPrefix("/kafka").Handler(kfkHandler)

	// wetsnow.com
	wsHandler := std.Handler("wetsnow.com", mdlw,
		handlers.CombinedLoggingHandler(logFile,
			http.FileServer(http.Dir(path.Join(*dataDir, "wetsnow.com")))),
	)
	r.Host("{subdomain:[a-z]*}.wetsnow.com").Handler(wsHandler)

	// galinasbeautyroom.com
	r.Host("galinasbeautyroom.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.galinasbeautyroom.com/", http.StatusMovedPermanently)
	})
	gsbHandler := std.Handler("galinasbeautyroom.com", mdlw,
		handlers.CombinedLoggingHandler(logFile,
			http.FileServer(http.Dir(path.Join(*dataDir, "galinasbeautyroom.com")))),
	)
	r.Host("{subdomain:[a-z]*}.galinasbeautyroom.com").Handler(gsbHandler)

	// dusselskolk.com
	r.Host("dusselskolk.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.dusselskolk.com/", http.StatusMovedPermanently)
	})
	dsHandler := std.Handler("dusselskolk.com", mdlw,
		handlers.CombinedLoggingHandler(logFile,
			http.FileServer(http.Dir(path.Join(*dataDir, "dusselskolk.com")))),
	)
	r.Host("{subdomain:[a-z]*}.dusselskolk.com").Handler(dsHandler)

	// Root
	r.PathPrefix("/").Handler(handlers.CombinedLoggingHandler(logFile, &RootHandler{}))
}

func (h RootHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "nothing here?\n")
}

// HandleHealthz handles health checks.
func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

// HandleServez handles ready to server checks.
func HandleServez(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

// HandleEnv reports back on request headers
func HandleEnv(w http.ResponseWriter, r *http.Request) {
	requestDump, err := httputil.DumpRequest(r, true)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Fprintln(w, "<pre>")
	fmt.Fprintf(w, string(requestDump))
	fmt.Fprintln(w, "</pre>")
}

// tracingHandler calls handler and traces the execution
type tracingHandler struct {
	tracer trace.Tracer
}

// Middleware implements the Gorilla MUX middleware interface
func (h *tracingHandler) Middleware(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h.traceRequest(w, req)
		next.ServeHTTP(w, req)
	})
}

func (h *tracingHandler) traceRequest(w http.ResponseWriter, req *http.Request) {

	ctx := req.Context()
	// span := trace.SpanFromContext(ctx)
	// defer span.End()
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(HTTPMethodKey.String(req.Method))
	span.SetAttributes(HTTPTargetKey.String(req.URL.Path))
	span.SetAttributes(HTTPSchemeKey.String(req.URL.Scheme))
	span.SetAttributes(HTTPFlavorKey.String(req.Proto))
	span.SetAttributes(HTTPServerNameKey.String(req.Host))
	span.SetAttributes(HTTPRequestContentLengthKey.Int64(req.ContentLength))
	span.SetAttributes(HTTPURLKey.String(req.URL.Opaque))
	span.SetAttributes(NetPeerIPKey.String(req.RemoteAddr))
	span.SetAttributes(HTTPUserAgentKey.String(req.UserAgent()))
	span.SetAttributes(HTTPStatusCodeKey.Int(http.StatusOK))

	// _, span := h.tracer.Start(ctx, "/",
	// 	trace.WithAttributes(
	// 		// https://pkg.go.dev/go.opentelemetry.io/otel/semconv
	// 		HTTPMethodKey.String(req.Method),
	// 		HTTPTargetKey.String(req.URL.Path),
	// 		HTTPSchemeKey.String(req.URL.Scheme),
	// 		HTTPFlavorKey.String(req.Proto),
	// 		HTTPServerNameKey.String(req.Host),
	// 		HTTPRequestContentLengthKey.Int64(req.ContentLength),
	// 		// HTTPFlavorKey.String(req.),
	// 		HTTPURLKey.String(req.URL.Opaque),
	// 		NetPeerIPKey.String(req.RemoteAddr),
	// 		HTTPStatusCodeKey.Int(200),
	// 		HTTPUserAgentKey.String(req.UserAgent()),
	// 	),
	// )
	defer span.End()
}
