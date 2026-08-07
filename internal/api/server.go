// Package api exposes the chain over HTTP so it can be driven from Postman,
// curl or any other client.
//
// It is transport only. Every rule about what a valid transaction or block is
// lives in internal/chain and its dependencies; this package parses JSON, takes
// a lock, calls one method, and encodes the answer. If you find blockchain logic
// in here, it is in the wrong file.
//
// SECURITY: there is no authentication. The server signs transfers with keys
// from the local keystore, so anyone who can reach it can spend those coins.
// That is why it binds to loopback by default, and why this is a development
// convenience rather than a node anyone should expose.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"toychain/internal/chain"
	"toychain/internal/config"
	"toychain/internal/ledger"
	"toychain/internal/store"
	"toychain/internal/wallet"
)

// Server holds the single chain every request operates on.
type Server struct {
	// mu serialises requests. The chain is a single append-only structure and
	// mining mutates it, so one writer at a time is both the simplest and the
	// only correct choice here.
	mu      sync.Mutex
	chain   *chain.Chain
	store   *store.Store
	wallet  *wallet.Keystore
	cfg     config.Config
	started time.Time
}

// New wires a server around an already-loaded chain.
func New(cfg config.Config, c *chain.Chain, s *store.Store, ks *wallet.Keystore) *Server {
	return &Server{chain: c, store: s, wallet: ks, cfg: cfg, started: time.Now()}
}

// Routes returns the HTTP handler. Patterns use the method-aware routing added
// to net/http in Go 1.22, so no third-party router is needed.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /chain", s.getChain)
	mux.HandleFunc("GET /chain/validate", s.validate)
	mux.HandleFunc("GET /blocks/{height}", s.getBlock)
	mux.HandleFunc("GET /balances", s.getBalances)
	mux.HandleFunc("GET /balances/{account}", s.getBalance)
	mux.HandleFunc("GET /pending", s.getPending)
	mux.HandleFunc("GET /keys", s.listKeys)
	mux.HandleFunc("POST /keys", s.createKey)
	mux.HandleFunc("POST /faucet", s.faucet)
	mux.HandleFunc("POST /transactions", s.createTransaction)
	mux.HandleFunc("POST /mine", s.mine)
	return logRequests(jsonErrors(mux))
}

// jsonErrors rewrites the plain-text 404 and 405 replies that net/http produces
// for unmatched routes into the same {"error": ...} shape every handler here
// uses. A client should never have to parse two different error formats from one
// API just because one came from the router.
type errorRewriter struct {
	http.ResponseWriter
	rewrite bool
}

func (w *errorRewriter) WriteHeader(code int) {
	// Handlers in this package set the JSON content type before writing the
	// header, so anything still untyped at 404/405 came from the router.
	if (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) &&
		w.Header().Get("Content-Type") != "application/json" {
		w.rewrite = true
		w.Header().Set("Content-Type", "application/json")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorRewriter) Write(b []byte) (int, error) {
	if !w.rewrite {
		return w.ResponseWriter.Write(b)
	}
	w.rewrite = false
	body, err := json.Marshal(map[string]string{"error": strings.TrimSpace(string(b))})
	if err != nil {
		return w.ResponseWriter.Write(b)
	}
	if _, err := w.ResponseWriter.Write(body); err != nil {
		return 0, err
	}
	// Report the caller's byte count, not ours: net/http compares this against
	// the length it handed us.
	return len(b), nil
}

func jsonErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&errorRewriter{ResponseWriter: w}, r)
	})
}

// ListenAndServe runs until ctx is cancelled, then shuts down gracefully so an
// in-flight mine is not cut off mid-block.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.Routes(),
		// Mining a block can legitimately take a while, so the write timeout is
		// generous; the read timeout stays short because bodies are tiny.
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// --- helpers -----------------------------------------------------------------

// writeJSON is the single place a response is encoded, so every endpoint answers
// in the same shape and with the same headers.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// writeError answers with {"error": "..."}, the only error shape this API uses.
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// readJSON decodes a request body, rejecting unknown fields so a typo in a
// Postman request is reported rather than silently ignored.
func readJSON(r *http.Request, target any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// persist saves the chain after a mutation. A write failure is reported rather
// than swallowed: the in-memory chain would otherwise disagree with the file.
func (s *Server) persist() error {
	return s.store.Save(s.chain.Blocks(), s.chain.Pending())
}

// logRequests prints one line per request. Enough to follow along in a terminal
// while clicking through Postman, and no more.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// account resolves a label from the keystore to an address, passing raw
// addresses through unchanged, and annotates it with a label if we know one.
func (s *Server) account(labelOrAddress string) (address, label string) {
	address = s.wallet.Resolve(labelOrAddress)
	return address, s.wallet.LabelFor(address)
}

// accountView is how an account appears in every response that mentions one.
type accountView struct {
	Address string `json:"address"`
	Label   string `json:"label,omitempty"`
	Balance int64  `json:"balance"`
}

// txView keeps transaction responses consistent, and adds the ID, which is
// derived rather than stored.
func txView(tx ledger.Transaction) map[string]any {
	return map[string]any{
		"id":        tx.ID(),
		"from":      tx.From,
		"to":        tx.To,
		"amount":    tx.Amount,
		"timestamp": tx.Timestamp,
		"coinbase":  tx.IsCoinbase(),
	}
}
