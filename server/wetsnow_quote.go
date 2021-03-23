package server

import (
	"flag"
	"fmt"
	"io"
	"net/http"

	// "go.opentelemetry.io/contrib/instrumentation/net/http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var (
	quoteServer = flag.String("quote_server", "localhost:8080", "http address of the quote server")
)

type quoteHandler struct {
	tracer     trace.Tracer
	httpClient *http.Client
}

func newQuoteHandler(t trace.Tracer) *quoteHandler {
	return &quoteHandler{
		tracer:     t,
		httpClient: &http.Client{},
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

	req, err := http.NewRequestWithContext(req.Context(), "GET", *quoteServer, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Until https://www.honeycomb.io/blog/from-0-to-insight-with-opentelemetry-in-go/ works properly
	// Inject trace headers
	tc := otel.GetTextMapPropagator()
	tc.Inject(req.Context(), propagation.HeaderCarrier(req.Header))

	// Make http call
	resp, err := qh.httpClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	fmt.Fprintf(w, string(body))
}
