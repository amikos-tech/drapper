.PHONY: test
test:
	go test -tags=test -v ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: lint-fix
lint-fix:
	golangci-lint run --fix --skip-dirs=./swagger ./...

.PHONY: clean-lint-cache
clean-lint-cache:
	golangci-lint cache clean