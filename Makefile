.PHONY: test

test:
	go test -timeout 30m -cover -coverprofile=coverage.out ./...
