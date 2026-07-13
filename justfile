# onair - dev tasks

default: test

build:
    go build ./...

test:
    go test ./...

lint:
    go vet ./...
    test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }

install:
    go install ./cmd/onair
