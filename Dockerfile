FROM golang:alpine as builder

WORKDIR /app/
WORKDIR $GOPATH/src/github.com/kamackay/dns

RUN apk upgrade --update --no-cache
#RUN apk add --no-cache git

ADD ./go.mod ./

RUN go mod download && go mod verify

ADD ./ ./

RUN go build -o server.file ./*.go && cp ./server.file /app/

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server.file /app/server

CMD ["./server"]


