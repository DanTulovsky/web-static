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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/enriquebris/goconcurrentqueue"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel/trace"
)

const (
	kafkaQueueMaxSize = 10
)

var (
	enableLogs  = flag.Bool("enable_logging", true, "Set to enable logging.")
	enableKafka = flag.Bool("enable_kafka", false, "Set to true to enable kafka support")
	logDir      = flag.String("log_dir", "", "Top level directory for log files, if empty (and enable_logging) logs to stdout")
	dataDir     = flag.String("data_dir", "data/hosts", "Top level directory for site files.")
	pprofPort   = flag.String("pprof_port", "6060", "port for pprof")
	addr        = flag.String("http_port", ":8080", "The address to listen on for HTTP requests.")
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
	log.Println("Enabling logging...")
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
	} else {
		s.RegisterHandlers(nil)
	}
	go func() {
		err := s.startPprof()
		if err != nil {

		}
	}()

	log.Printf("Staritng http server on %v", *addr)
	return s.Srv.ListenAndServe()
}

func (s *Server) kafkaSubscribe(kafkaQueue goconcurrentqueue.Queue) {
	log.Println("Starting kafka consumer...")
	c := newKafkaConsumer()
	defer func() {
		if err := c.Close(); err != nil {
			log.Println(err)
		}
	}()

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

// setHeaders sets shared headers
func setHeaders(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// w.Header().Set("Permissions-Policy", "")

		next.ServeHTTP(w, r)
	}
}

// RegisterHandlers registers http handlers
func (s *Server) RegisterHandlers(kafkaQueue goconcurrentqueue.Queue) {
	logFile := enableLogging()

	r := mux.NewRouter()
	s.Srv.Handler = r
	r.Use(otelmux.Middleware("web-static"))

	// Health Checks used by kubernetes
	r.HandleFunc("/healthz", HandleHealthz)
	r.HandleFunc("/servez", HandleServez)
	r.HandleFunc("/env", HandleEnv)
	r.HandleFunc("/auth/", HandleEnv)
	r.Handle("/metrics", promhttp.Handler())

	// wetsnow.com redirect
	r.Host("wetsnow.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.wetsnow.com/", http.StatusMovedPermanently)
	})

	if *enableKafka {
		// wetsnow.com/kafka
		kfkHandler := handlers.CombinedLoggingHandler(logFile, newKafkaHandler(kafkaQueue))
		r.Host("www.wetsnow.com").PathPrefix("/kafka").Handler(kfkHandler)
	}

	// login.wetsnow.com
	loginHandler := handlers.CombinedLoggingHandler(logFile,
		setHeaders(http.FileServer(http.Dir(path.Join(*dataDir, "login.wetsnow.com")))))
	r.Host("login.wetsnow.com").PathPrefix("/").Handler(loginHandler)

	// wetsnow.com
	wsHandler := handlers.CombinedLoggingHandler(logFile,
		setHeaders(http.FileServer(http.Dir(path.Join(*dataDir, "wetsnow.com")))))

	// wetsnow.com/quote
	qtHandler := setHeaders(handlers.CombinedLoggingHandler(logFile, newQuoteHandler(s.tracer)))
	r.Host("{subdomain:[a-z]*}.wetsnow.com").PathPrefix("/quote").Handler(qtHandler)
	r.Host("{subdomain:[a-z]*}.wetsnow.com").PathPrefix("/").Handler(wsHandler)

	// galinasbeautyroom.com
	r.Host("galinasbeautyroom.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.galinasbeautyroom.com/", http.StatusMovedPermanently)
	})
	gsbHandler := handlers.CombinedLoggingHandler(logFile,
		setHeaders(http.FileServer(http.Dir(path.Join(*dataDir, "galinasbeautyroom.com")))))
	r.Host("{subdomain:[a-z]*}.galinasbeautyroom.com").PathPrefix("/").Handler(gsbHandler)

	// dusselskolk.com
	r.Host("dusselskolk.com").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.dusselskolk.com/", http.StatusMovedPermanently)
	})
	dsHandler := handlers.CombinedLoggingHandler(logFile,
		setHeaders(http.FileServer(http.Dir(path.Join(*dataDir, "dusselskolk.com")))))
	r.Host("{subdomain:[a-z]*}.dusselskolk.com").PathPrefix("/").Handler(dsHandler)

	// Root
	r.PathPrefix("/").Handler(setHeaders(handlers.CombinedLoggingHandler(logFile, &RootHandler{})))
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

// func (h *tracingHandler) traceRequest(w http.ResponseWriter, req *http.Request) {

// 	ctx := req.Context()
// 	tc := otel.GetTextMapPropagator()
// 	// Extract traceID from the headers
// 	ctxNew := tc.Extract(ctx, propagation.HeaderCarrier(req.Header))

// 	_, span := h.tracer.Start(ctxNew, "/")
// 	defer span.End()

// 	// log.Println(req.Header)
// 	// log.Print(span.SpanContext().TraceID)

// 	span.SetAttributes(HTTPMethodKey.String(req.Method))
// 	span.SetAttributes(HTTPTargetKey.String(req.URL.Path))
// 	span.SetAttributes(HTTPSchemeKey.String(req.Header.Get("X-Forwarded-Proto")))
// 	span.SetAttributes(HTTPFlavorKey.String(req.Proto))
// 	span.SetAttributes(HTTPServerNameKey.String(req.Host))
// 	span.SetAttributes(HTTPRequestContentLengthKey.Int64(req.ContentLength))
// 	span.SetAttributes(HTTPURLKey.String(fmt.Sprintf("%v://%v%v", req.Header.Get("X-Forwarded-Proto"), req.Host, req.URL.RequestURI())))
// 	span.SetAttributes(NetPeerIPKey.String(req.RemoteAddr))
// 	span.SetAttributes(HTTPUserAgentKey.String(req.UserAgent()))
// 	span.SetAttributes(HTTPStatusCodeKey.Int(http.StatusOK))

// _, span := h.tracer.Start(ctx, "/",
// 	trace.WithAttributes(
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
// }
