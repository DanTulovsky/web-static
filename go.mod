module github.com/DanTulovsky/web-static

go 1.16

require (
	github.com/DanTulovsky/quote-server v0.0.14
	github.com/confluentinc/confluent-kafka-go v1.6.1
	github.com/enriquebris/goconcurrentqueue v0.6.0
	github.com/google/uuid v1.2.0
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/lightstep/otel-launcher-go v0.18.0
	go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux v0.18.0
	// go.opentelemetry.io/contrib v0.19.0 // indirect
	// go.opentelemetry.io/contrib/propagators v0.19.0 // indirect
	go.opentelemetry.io/otel v0.18.0
	go.opentelemetry.io/otel/trace v0.18.0
	google.golang.org/grpc v1.36.0
)
