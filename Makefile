.PHONY: build test vet run web binary clean clean-web

BIN := bin/gbg
EMBED := internal/webui/dist

# Go-only build (serve falls back to --web-dir / API-only).
build:
	go build -o $(BIN) ./cmd/gbg

test:
	go test ./...

vet:
	go vet ./...

# make run URL=<github-url-or-path> [BRANCH=dev] [REPO=owner/name]
run:
	go run ./cmd/gbg ingest $(URL) \
		$(if $(BRANCH),--default-branch $(BRANCH),) \
		$(if $(REPO),--repo $(REPO),)

# Build the SPA and stage it for embedding.
web:
	cd web && npm install && npm run build
	rm -rf $(EMBED)
	mkdir -p $(EMBED)
	cp -R web/dist/. $(EMBED)/

# Single self-contained binary: SPA embedded via internal/webui.
binary: web
	go build -o $(BIN) ./cmd/gbg
	@echo "built $(BIN) with embedded SPA"

clean-web:
	rm -rf $(EMBED)
	mkdir -p $(EMBED)
	touch $(EMBED)/.gitkeep

clean: clean-web
	rm -rf bin data/*/
