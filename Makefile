# Developer shortcuts. Every target is a one-liner you can also run by hand,
# so `make` is a convenience, never a requirement.
BINARY := tbc
PKG    := ./cmd/tbc

.PHONY: build test fmt vet check run clean docker docker-run

build:            ## Compile the CLI into ./bin
	go build -o bin/$(BINARY) $(PKG)

test:             ## Run the whole test suite
	go test ./...

fmt:              ## Format every file in place
	gofmt -w .

vet:              ## Report suspicious constructs
	go vet ./...

check: fmt vet test  ## What CI would run

run: build        ## Build then run, e.g. make run ARGS="print"
	./bin/$(BINARY) $(ARGS)

docker:           ## Build the image (vet and tests run inside the build)
	docker build -t toychain/tbc .

docker-run: docker  ## Run a command in the image, e.g. make docker-run ARGS="mine"
	docker run --rm -v chain-data:/data toychain/tbc $(ARGS)

clean:
	rm -rf bin data
