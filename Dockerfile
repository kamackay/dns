FROM golang:alpine as builder

WORKDIR /app/
WORKDIR $GOPATH/src/github.com/kamackay/dns

RUN apk upgrade --update --no-cache
#RUN apk add --no-cache git

ADD ./go.mod ./

RUN go mod download && go mod verify

ADD ./ ./

RUN go build -o server ./*.go && cp ./server /app/

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server /app/

CMD ["./server"]


