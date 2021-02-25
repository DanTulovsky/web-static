package server

import (
	"fmt"
	"net/http"

	"github.com/confluentinc/confluent-kafka-go/kafka"
)

const (
	kafkaBroker = "kafka0-headless.kafka:9092"
	kafkaGroup  = "web1"
	kafkaTopic  = "otlp_spans"
)

type kafkaHandler struct {
	consumer *kafka.Consumer
}

// newKafkaHandler returns a new kafka handler
func newKafkaHandler() *kafkaHandler {

	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": kafkaBroker,
		"group.id":          kafkaGroup,
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		panic(err)
	}

	topics := []string{kafkaTopic}
	c.SubscribeTopics(topics, nil)

	return &kafkaHandler{
		consumer: c,
	}
}

func (kh *kafkaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "kafka")

	ev := kh.consumer.Poll(100)
	if ev == nil {
		fmt.Fprintln(w, "got nothing...")
		return
	}

	switch e := ev.(type) {
	case *kafka.Message:
		fmt.Fprintf(w, "%% Message on %s:\n%s\n",
			e.TopicPartition, string(e.Value))
		if e.Headers != nil {
			fmt.Fprintf(w, "%% Headers: %v\n", e.Headers)
		}
	case kafka.Error:
		// Errors should generally be considered
		// informational, the client will try to
		// automatically recover.
		// But in this example we choose to terminate
		// the application if all brokers are down.
		fmt.Fprintf(w, "%% Error: %v: %v\n", e.Code(), e)
	default:
	}

	// msg, err := kh.consumer.ReadMessage(time.Millisecond * 10)
	// if err == nil {
	// 	fmt.Fprintf(w, "Message on %s: %s\n", msg.TopicPartition, string(msg.Value))
	// } else {
	// 	fmt.Fprintf(w, "err talking to kafka: %v", err)
	// }
}
