.PHONY: setup run seed build test clean

# Isolated downloader dependency for the on-demand worker.
setup:
	python3 -m venv .venv
	.venv/bin/python -m pip install 'spotdl==4.5.2'

# Import the demo songs, then serve on http://localhost:8080 with the settings in .env
# (see .env.example).
run: seed
	@set -a; [ ! -f .env ] || . ./.env; set +a; go run ./cmd/mifs-server serve

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
