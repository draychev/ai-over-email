.PHONY: run list test

run:
	@mkdir -p .tmp
	@go build -o .tmp/mailwatch ./cmd
	@.tmp/mailwatch

list:
	@go run ./cmd/maillist

test:
	@go test ./...
