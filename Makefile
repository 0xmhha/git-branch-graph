.PHONY: build test vet run clean

BIN := bin/gbg

build:
	go build -o $(BIN) ./cmd/gbg

test:
	go test ./...

vet:
	go vet ./...

# make run URL=<github-url-or-path> [BRANCH=dev]
run:
	go run ./cmd/gbg ingest $(URL) $(if $(BRANCH),--default-branch $(BRANCH),)

clean:
	rm -rf bin data/*/
