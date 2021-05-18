FROM golang:1.16-alpine

WORKDIR /go/src/app
COPY . .

RUN apk add git && go get -d -v ./...
RUN go install -v ./...

CMD ["matrix-websockets-proxy"]