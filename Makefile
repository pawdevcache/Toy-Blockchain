# Developer shortcuts. Every target is a one-liner you can also run by hand,
# so `make` is a convenience, never a requirement.
BINARY := tbc
PKG    := ./cmd/tbc

.PHONY: build test fmt vet check run clean

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

clean:
	rm -rf bin data
