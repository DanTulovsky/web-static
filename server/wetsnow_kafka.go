package server

import (
	"fmt"
	"net/http"
	"time"

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

	msg, err := kh.consumer.ReadMessage(time.Millisecond * 10)
	if err == nil {
		fmt.Fprintf(w, "Message on %s: %s\n", msg.TopicPartition, string(msg.Value))
	} else {
		fmt.Fprintf(w, "err talking to kafka: %v", err)
	}
}
