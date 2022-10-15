package server

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.11.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"

	_ "google.golang.org/grpc/xds"

	pb "github.com/DanTulovsky/quote-server/proto"
)

var (
	quoteServer     = flag.String("quote_server", "localhost:8080", "http address of the quote server")
	quoteServerGRPC = flag.String("quote_server_grpc", "localhost:8081", "grpc address of the quote server")
	// For director, use: xds:///quote-server-gke:8000
	quoteServerUseGRPC = flag.Bool("quote_server_use_grpc", true, "Set to use grpc to talk to quote server")
	xdsCreds           = flag.Bool("xds_creds", false, "whether the server should use xDS APIs to receive security configuration")
)

type quoteHandler struct {
	tracer     trace.Tracer
	httpClient *http.Client
	grpcClient pb.QuoteClient
}

func newQuoteHandler(t trace.Tracer) *quoteHandler {
	log.Printf("Connecting to quote server on: %v", *quoteServerGRPC)
	creds := insecure.NewCredentials()

	if *xdsCreds {
		log.Println("Using xDS credentials...")
		var err error
		if creds, err = xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()}); err != nil {
			log.Fatalf("failed to create client-side xDS credentials: %v", err)
		}
	}

	conn, err := grpc.Dial(*quoteServerGRPC,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	c := pb.NewQuoteClient(conn)

	return &quoteHandler{
		tracer:     t,
		httpClient: &http.Client{},
		grpcClient: c,
	}
}

func (qh *quoteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {

	// // gorrilla mux middleware makes sure the trace is in the context
	// _, span := qh.tracer.Start(req.Context(), "quote-handler")
	// defer span.End()

	w.Header().Add("Content-Type", "text/html")

	var qfunc func(context.Context) (string, error)

	switch *quoteServerUseGRPC {
	case true:
		qfunc = qh.getQuoteGRPC
	default:
		qfunc = qh.getQuote
	}

	log.Printf("talking to quote server on: %v", *quoteServerGRPC)
	quote, err := qfunc(req.Context())
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = fmt.Fprintf(w, quote)
	if err != nil {
		log.Printf("error writing to output: %v", err)
	}
}

func (qh *quoteHandler) getQuoteGRPC(ctx context.Context) (string, error) {
	_, span := qh.tracer.Start(ctx, "getQuoteGRPC",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPMethodKey.String("GET"),
			semconv.HTTPURLKey.String(*quoteServerGRPC),
		),
	)
	defer span.End()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	span.AddEvent("Retrieving quote (via grpc)")
	r, err := qh.grpcClient.GetQuote(ctx, &pb.GetQuoteRequest{})
	if err != nil {
		span.RecordError(fmt.Errorf("could could get quote: %v", err))
		return "", err
	}
	span.AddEvent("Retrieved quote")
	span.SetAttributes(semconv.RPCMethodKey.String("GetQuote"))

	return r.GetQuoteText(), nil
}

func (qh *quoteHandler) getQuote(ctx context.Context) (string, error) {
	_, span := qh.tracer.Start(ctx, "getQuote",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPMethodKey.String("GET"),
			semconv.HTTPURLKey.String(*quoteServer),
		),
	)
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, "GET", *quoteServer, nil)
	if err != nil {
		return "", err
	}

	// Until https://www.honeycomb.io/blog/from-0-to-insight-with-opentelemetry-in-go/ works properly
	// Inject trace headers
	tc := otel.GetTextMapPropagator()
	tc.Inject(req.Context(), propagation.HeaderCarrier(req.Header))

	// Make http call
	span.AddEvent("Retrieving quote (via http)")
	resp, err := qh.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("error closing body: %v", err)
		}
	}()

	span.AddEvent("Retrieved quote")

	span.SetAttributes(semconv.HTTPStatusCodeKey.Int(resp.StatusCode))
	body, err := io.ReadAll(resp.Body)

	return string(body), err
}
