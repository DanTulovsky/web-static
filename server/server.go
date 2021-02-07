package server

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/b3"
)

// gcr.io/snowcloud-01/static-web/frontend:YYYYMMDD00

var (
	enableLogs    = flag.Bool("enable_logging", true, "Set to enable logging.")
	enableTracing = flag.Bool("enable_tracing", false, "Set to true to enable tracing to jaeger.")
	logDir        = flag.String("log_dir", "", "Top level directory for log files, if empty (and enable_logging) logs to stdout")
	dataDir       = flag.String("data_dir", "data/hosts", "Top level directory for site files.")
	pprofPort     = flag.String("pprof_port", "6060", "port for pprof")
	addr          = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
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
	router *mux.Router
}

// NewServer returns a server
func NewServer() (*Server, error) {

	router := mux.NewRouter()

	// Now use the logger with your http.Server:
	logger := log.New(debugLogger{}, "", 0)

	return &Server{
		Srv: &http.Server{
			Handler: &ochttp.Handler{
				Handler:     router,
				Propagation: &b3.HTTPFormat{},
			},
			Addr:         *addr,
			WriteTimeout: 15 * time.Second,
			ReadTimeout:  15 * time.Second,
			IdleTimeout:  time.Second * 60,
			ErrorLog:     logger,
		},
		router: router,
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
	s.RegisterHandlers()

	go s.startPprof()

	log.Printf("Staritng http server on %v", *addr)
	return s.Srv.ListenAndServe()
}

func (s *Server) startPprof() error {
	log.Printf("Starting pprof on port %v", *pprofPort)
	pprofMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()

	log.Println(http.ListenAndServe(fmt.Sprintf("localhost:%s", *pprofPort), pprofMux))

	return nil
}

// RegisterHandlers registers http handlers
func (s *Server) RegisterHandlers() {

	mdlw := middleware.New(middleware.Config{
		Recorder:      metrics.NewRecorder(metrics.Config{}),
		Service:       "web-static",
		GroupedStatus: true,
	})

	r := s.router
	r.Use(std.HandlerProvider("default", mdlw))

	logFile := enableLogging()

	// Prometheus metrics
	r.Handle("/metrics", std.Handler("/metrics", mdlw,
		handlers.CombinedLoggingHandler(logFile, promhttp.Handler())))

	// Health Checks used by kubernetes
	r.HandleFunc("/healthz", HandleHealthz)
	r.HandleFunc("/servez", HandleServez)

	// wetsnow.com
	r.Host("wetsnow.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.wetsnow.com/", http.StatusMovedPermanently)
	})
	wsHandler := std.Handler("wetsnow.com", mdlw, tracingHandler{
		handler: handlers.CombinedLoggingHandler(
			logFile,
			http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "wetsnow.com"))))),
	})
	r.Host("{subdomain:[a-z]*}.wetsnow.com").Handler(wsHandler)

	// galinasbeautyroom.com
	r.Host("galinasbeautyroom.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.galinasbeautyroom.com/", http.StatusMovedPermanently)
	})
	gsbHandler := std.Handler("galinasbeautyroom.com", mdlw,
		tracingHandler{
			handler: handlers.CombinedLoggingHandler(
				logFile,
				http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "galinasbeautyroom.com"))))),
		})

	r.Host("{subdomain:[a-z]*}.galinasbeautyroom.com").Handler(gsbHandler)

	// dusselskolk.com
	r.Host("dusselskolk.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.dusselskolk.com/", http.StatusMovedPermanently)
	})
	dsHandler := std.Handler("dusselskolk.com", mdlw, tracingHandler{
		handler: handlers.CombinedLoggingHandler(
			logFile,
			http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "dusselskolk.com"))))),
	})
	r.Host("{subdomain:[a-z]*}.dusselskolk.com").Handler(dsHandler)

	// Root
	rh := &RootHandler{}
	r.PathPrefix("/").Handler(
		tracingHandler{handler: handlers.CombinedLoggingHandler(logFile, ochttp.WithRouteTag(rh, "/"))})
}

func (h RootHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "nothing here\n")
}

// HandleHealthz handles health checks.
func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

// HandleServez handles ready to server checks.
func HandleServez(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

// tracingHandler calls handler and traces the execution
type tracingHandler struct {
	handler http.Handler
}

func (h tracingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if *enableTracing {
		// trace here
		tracer := opentracing.GlobalTracer()

		var span opentracing.Span

		ectx, err := tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(req.Header))
		if err != nil {
			log.Println(err)
			span = opentracing.StartSpan("/")
		} else {

			span = opentracing.StartSpan("/", ext.RPCServerOption(ectx))
		}

		span.SetTag("user_agent", req.UserAgent())
		ext.SpanKindRPCServer.Set(span)
		ext.HTTPMethod.Set(span, req.Method)
		// ext.HTTPStatusCode.Set(span, uint16(r.))
		ext.HTTPUrl.Set(span, req.RequestURI)
		span.SetTag("host", req.Host)

		defer span.Finish()
	}

	// call real handler
	h.handler.ServeHTTP(w, req)
	if req.MultipartForm != nil {
		req.MultipartForm.RemoveAll()
	}
}
