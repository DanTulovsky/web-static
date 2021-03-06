package server

// TODO: #1 Become a free consumer and subscribe to all partitions in the topic
// Partitions may be manually assigned to free consumers. If necessary, an entire
// topic may be assigned to a single free consumer — this is done by individually
// assigning all partitions. (Consumer 1 can be freely assigned any partition.)

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/enriquebris/goconcurrentqueue"
	"github.com/google/uuid"
	// tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	kafkaTopic = "otlp_spans"
)

var (
	kafkaGroup  = uuid.New().String()
	kafkaBroker = flag.String("kafka_broker", "localhost:9092", "kafka broker")
)

type kafkaHandler struct {
	kafkaQueue goconcurrentqueue.Queue
}

// newKafkaHandler returns a new kafka handler
func newKafkaHandler(q goconcurrentqueue.Queue) *kafkaHandler {

	return &kafkaHandler{
		kafkaQueue: q,
	}
}

func newKafkaConsumer() *kafka.Consumer {

	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": *kafkaBroker,
		"group.id":          kafkaGroup,
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		panic(err)
	}

	topics := []string{kafkaTopic}
	c.SubscribeTopics(topics, nil)

	return c
}

func (kh *kafkaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html")

	fmt.Fprintf(w, "<p>kafka -> [len before: %v]</p>\n", kh.kafkaQueue.GetLen())

	value, err := kh.kafkaQueue.Dequeue()
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	fmt.Fprintf(w, "%v\n", value.(string))

	// valueString := value.(string)

	// event := &tracepb.ExportTraceServiceRequest{}
	// if err := proto.Unmarshal([]byte(valueString), event); err != nil {
	// 	fmt.Fprintf(w, "unmarshal error: %v", err)
	// 	return
	// }

	fmt.Fprintf(w, "<p>kafka -> [len after: %v]</p>\n", kh.kafkaQueue.GetLen())
}
