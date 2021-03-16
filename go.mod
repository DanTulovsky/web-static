module github.com/DanTulovsky/web-static

go 1.16

require (
	github.com/confluentinc/confluent-kafka-go v1.6.1
	github.com/enriquebris/goconcurrentqueue v0.6.0
	github.com/google/uuid v1.2.0
	github.com/gorilla/handlers v1.5.1
	github.com/gorilla/mux v1.8.0
	github.com/lightstep/otel-launcher-go/launcher v0.0.0-00010101000000-000000000000
	github.com/prometheus/client_golang v1.9.0
	github.com/slok/go-http-metrics v0.9.0
	go.opentelemetry.io/otel v0.18.0
	go.opentelemetry.io/otel/trace v0.18.0
)

replace github.com/lightstep/otel-launcher-go/launcher => /Users/dant/go/src/github.com/lightstep/otel-launcher-go/launcher

replace github.com/lightstep/otel-launcher-go => /Users/dant/go/src/github.com/lightstep/otel-launcher-go
