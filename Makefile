.PHONY: run run-usenet list mcp test

run:
	@mkdir -p .tmp
	@go build -o .tmp/mailwatch ./cmd
	@.tmp/mailwatch

run-usenet:
	@mkdir -p .tmp
	@go build -o .tmp/usenetwatch ./cmd/usenetwatch
	@.tmp/usenetwatch

list:
	@go run ./cmd/maillist

mcp:
	@go run ./cmd/fastmail-mcp

test:
	@go test ./...
