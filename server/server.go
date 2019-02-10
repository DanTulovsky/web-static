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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/plugin/ochttp/propagation/b3"
	"go.opencensus.io/stats/view"
)

// gcr.io/snowcloud-01/static-web/frontend:YYYYMMDD00

var (
	enableLogs = flag.Bool("enable_logging", false, "Set to enable logging.")
	logDir     = flag.String("log_dir", "", "Top level directory for log files, if empty (and enable_logging) logs to stdout")
	dataDir    = flag.String("data_dir", "data", "Top level directory for site files.")
)

// RootHandler handles requests
type RootHandler struct {
}

func newRootHandler() *RootHandler {

	return &RootHandler{}
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
func NewServer(addr string) (*Server, error) {

	router := mux.NewRouter()

	// Now use the logger with your http.Server:
	logger := log.New(debugLogger{}, "", 0)

	return &Server{
		Srv: &http.Server{
			Handler: &ochttp.Handler{
				Handler:     router,
				Propagation: &b3.HTTPFormat{},
			},
			Addr:         addr,
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

// RegisterHandlers registers http handlers
func (s *Server) RegisterHandlers() {

	r := s.router

	logFile := enableLogging()

	// Prometheus metrics
	r.Handle("/metrics", handlers.CombinedLoggingHandler(logFile, promhttp.Handler()))

	// Health Checks used by kubernetes
	r.HandleFunc("/healthz", HandleHealthz)
	r.HandleFunc("/servez", HandleServez)

	// wetsnow.com
	r.Host("wetsnow.com").Handler(handlers.CombinedLoggingHandler(logFile,
		http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "wetsnow.com"))))))
	r.Host("{subdomain:[a-z]*}.wetsnow.com").Handler(handlers.CombinedLoggingHandler(logFile,
		http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "wetsnow.com"))))))

	// galinasbeautyroom.com
	r.Host("galinasbeautyroom.com").Handler(handlers.CombinedLoggingHandler(logFile,
		http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "galinasbeautyroom.com"))))))
	r.Host("{subdomain:[a-z]*}.galinasbeautyroom.com").Handler(handlers.CombinedLoggingHandler(logFile,
		http.FileServer(http.Dir(fmt.Sprintf(path.Join(*dataDir, "galinasbeautyroom.com"))))))

	// Root
	rh := newRootHandler()
	r.PathPrefix("/").Handler(
		handlers.CombinedLoggingHandler(logFile, ochttp.WithRouteTag(rh, "/")))
}

// EnableViews sets up views -> stackdriver metrics
func EnableViews() {
	// Register OpenCensus server views. These map to Stackdriver metrics.
	if err := view.Register(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register server views for HTTP metrics: %v", err)
	}
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
