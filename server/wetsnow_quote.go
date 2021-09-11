package server

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"

	// "go.opentelemetry.io/contrib/instrumentation/net/http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"

	pb "github.com/DanTulovsky/quote-server/proto"
)

var (
	quoteServer     = flag.String("quote_server", "localhost:8080", "http address of the quote server")
	quoteServerGRPC = flag.String("quote_server_grpc", "localhost:8081", "grpc address of the quote server")
)

type quoteHandler struct {
	tracer     trace.Tracer
	httpClient *http.Client
	grpcClient pb.QuoteClient
}

func newQuoteHandler(t trace.Tracer) *quoteHandler {
	conn, err := grpc.Dial(*quoteServerGRPC, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewQuoteClient(conn)

	return &quoteHandler{
		tracer:     t,
		httpClient: &http.Client{},
		grpcClient: c,
		// httpClient: http.Client{
		// 	Transport: otelhttp.NewTransport(http.DefaulTransport),
		// },
	}
}

func (qh *quoteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {

	// // gorrilla mux middleware makes sure the trace is in the context
	// _, span := qh.tracer.Start(req.Context(), "quote-handler")
	// defer span.End()

	w.Header().Add("Content-Type", "text/html")
	//quote, err := qh.getQuote(req.Context())
	quote, err := qh.getQuoteGRPC(req.Context())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, quote)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	defer resp.Body.Close()
	span.AddEvent("Retrieved quote")

	span.SetAttributes(semconv.HTTPStatusCodeKey.Int(resp.StatusCode))
	body, err := io.ReadAll(resp.Body)

	return string(body), err
}
