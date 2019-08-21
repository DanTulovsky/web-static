

#build stage
FROM golang:alpine AS builder

# install git and ca certs for debugger
RUN apk update && apk add --no-cache git ca-certificates

COPY . $GOPATH/src/github.com/DanTulovsky/web-static
WORKDIR $GOPATH/src/github.com/DanTulovsky/web-static

ADD . /go/src/github.com/DanTulovsky/web-static

# fetch deps
RUN go get -d -v

# optimized binary
# RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/run
RUN go build -o /go/bin/run
RUN go install -v 

# final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates bash
COPY --from=builder /go/bin/run /go/bin/run
COPY --from=builder /go/src/github.com/DanTulovsky/web-static/data /data/

# run this command automatically
# can do: docker run -it --rm --entrypoint=/bin/ash image...
ENTRYPOINT ["/go/bin/run"]

RUN mkdir -p /logs

# this is just documentation really
EXPOSE 8080
