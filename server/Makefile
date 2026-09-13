.PHONY: run seed build test clean

# Import the demo songs, then serve on http://localhost:8080.
run: seed
	go run ./cmd/mifs-server serve

# Import (or re-import) every song folder in seed/ into data/.
seed:
	go run ./cmd/mifs-server ingest seed

build:
	go build -o bin/mifs-server ./cmd/mifs-server

test:
	go vet ./...
	go test ./...

# Remove the generated catalog and media (seed/ is untouched).
clean:
	rm -rf data bin
