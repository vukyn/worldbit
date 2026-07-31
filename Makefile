#!make
include ./.env
export $(shell sed 's/=.*//' ./.env)

.PHONY: build build-headless run headless test golden bench vet tag

build:
	@go build -o bin/ .
build-headless:
	@go build -tags nogui -o bin/worldbit-headless .
run:
	@go run . --seed 1
headless:
	@go run . --headless --seed 1 --runs 100 --out runs.csv
test:
	@go test ./...
golden:
	@go test ./internal/sim -run TestGoldenHashes -update
bench:
	@go test ./internal/sim -bench . -benchtime 3x -run '^$$'
vet:
	@go vet ./...
tag:
	@[ -n "$(VERSION)" ] || { echo "Usage: make tag VERSION=x.y.z"; exit 1; }
	git tag -a v$(VERSION) -m "Release version $(VERSION)"
	git push origin v$(VERSION)
