package server

import (
	"fmt"
	"net/http"
)

type kafkaHandler struct{}

func (kh *kafkaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "kafka")
}
