package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"toychain/internal/chain"
	"toychain/internal/ledger"
)

// GET /health - is the node up, and where is the chain?
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"height":     s.chain.Height(),
		"blocks":     len(s.chain.Blocks()),
		"pending":    len(s.chain.Pending()),
		"difficulty": s.cfg.Difficulty,
		"uptime":     time.Since(s.started).Round(time.Second).String(),
	})
}

// GET /chain - every block, oldest first.
func (s *Server) getChain(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"height": s.chain.Height(),
		"blocks": s.chain.Blocks(),
	})
}

// GET /blocks/{height} - one block.
func (s *Server) getBlock(w http.ResponseWriter, r *http.Request) {
	height, err := strconv.ParseUint(r.PathValue("height"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("height must be a whole number"))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	blocks := s.chain.Blocks()
	if height >= uint64(len(blocks)) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no block at height %d; the tip is %d", height, s.chain.Height()))
		return
	}
	writeJSON(w, http.StatusOK, blocks[height])
}

// GET /chain/validate - re-check the whole chain.
//
// A broken chain answers 409 Conflict rather than 500: the server is working
// perfectly, it is the stored data that is wrong.
func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.chain.Validate()
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"valid":  true,
			"blocks": len(s.chain.Blocks()),
		})
		return
	}

	body := map[string]any{"valid": false, "error": err.Error()}
	var ve *chain.ValidationError
	if errors.As(err, &ve) {
		body["first_bad_block"] = ve.Height
		body["failed_check"] = ve.Check
		body["detail"] = ve.Detail
	}
	writeJSON(w, http.StatusConflict, body)
}

// GET /balances - every account with coins, plus the total supply.
func (s *Server) getBalances(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts := s.chain.Accounts()
	view := make([]accountView, 0, len(accounts))
	var total int64
	for _, a := range accounts {
		view = append(view, accountView{Address: a.Address, Label: s.wallet.LabelFor(a.Address), Balance: a.Balance})
		total += a.Balance
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": view, "total_supply": total})
}

// GET /balances/{account} - one account, by label or address.
func (s *Server) getBalance(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	address, label := s.account(r.PathValue("account"))
	writeJSON(w, http.StatusOK, accountView{
		Address: address,
		Label:   label,
		Balance: s.chain.Balance(address),
	})
}

// GET /pending - what would go into the next block.
func (s *Server) getPending(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pending := s.chain.Pending()
	view := make([]map[string]any, 0, len(pending))
	for _, tx := range pending {
		view = append(view, txView(tx))
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(view), "transactions": view})
}

// GET /keys - the local keystore.
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.wallet.Entries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	view := make([]accountView, 0, len(entries))
	for _, e := range entries {
		view = append(view, accountView{Address: e.Address, Label: e.Label, Balance: s.chain.Balance(e.Address)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": view})
}

// POST /keys {"label":"alice"} - create a key pair.
//
// The private key is never returned: it stays in the keystore file. An API that
// hands out private keys is a mistake worth not making even in a toy.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pair, err := s.wallet.Create(body.Label)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"label": body.Label, "address": pair.Address()})
}

// POST /faucet {"to":"alice","amount":100} - mint coins into an account.
func (s *Server) faucet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	address, label := s.account(body.To)
	if err := s.chain.Faucet(address, body.Amount); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// 202: accepted into the pool, not yet confirmed. Only mining confirms.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "queued", "to": address, "label": label, "amount": body.Amount,
		"note": "mine a block to confirm",
	})
}

// POST /transactions {"from":"alice","to":"bob","amount":30} - sign and queue.
//
// `from` must be a label this node holds the private key for: without the key
// there is no way to author a valid transfer at all, which is the entire point
// of signatures.
func (s *Server) createTransaction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sender, err := s.wallet.KeyPair(body.From)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	to, _ := s.account(body.To)

	tx := sender.Sign(ledger.NewTransfer("", to, body.Amount, time.Now().UnixNano()))
	if err := s.chain.AddTransaction(tx); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "queued", "transaction": txView(tx), "note": "mine a block to confirm",
	})
}

// POST /mine - do the proof of work and append a block.
//
// Uses the request context, so a client that gives up (Postman's Cancel, a
// closed connection) stops the search instead of leaving a core spinning.
func (s *Server) mine(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.chain.Mine(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"height":     result.Block.Height,
		"hash":       result.Block.Hash,
		"nonce":      result.Block.Nonce,
		"difficulty": result.Block.Difficulty,
		"hashes":     result.Hashes,
		"elapsed_ms": result.Elapsed.Milliseconds(),
		"hash_rate":  int64(result.HashRate()),
		"workers":    result.Workers,
		"txs":        len(result.Block.Transactions),
	})
}
