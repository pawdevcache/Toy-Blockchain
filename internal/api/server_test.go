package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"toychain/internal/chain"
	"toychain/internal/config"
	"toychain/internal/store"
	"toychain/internal/wallet"
)

// newTestServer returns a server backed by a temporary chain file and keystore.
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Difficulty = 2 // keep the suite fast
	cfg.DataFile = filepath.Join(dir, "chain.json")
	cfg.KeyFile = filepath.Join(dir, "keys.json")

	ks, err := wallet.Open(cfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, chain.New(cfg), store.New(cfg.DataFile), ks).Routes()
}

// call performs one request and decodes the JSON response.
func call(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("%s %s returned invalid JSON: %v\n%s", method, path, err, rec.Body.String())
		}
	}
	return rec.Code, decoded
}

// The walkthrough a reviewer will click through in Postman.
func TestHappyPath(t *testing.T) {
	h := newTestServer(t)

	if code, body := call(t, h, "GET", "/health", ""); code != 200 || body["status"] != "ok" {
		t.Fatalf("health: %d %v", code, body)
	}
	if code, _ := call(t, h, "POST", "/keys", `{"label":"alice"}`); code != 201 {
		t.Fatalf("create key: %d", code)
	}
	if code, _ := call(t, h, "POST", "/keys", `{"label":"bob"}`); code != 201 {
		t.Fatalf("create key: %d", code)
	}
	if code, _ := call(t, h, "POST", "/faucet", `{"to":"alice","amount":100}`); code != 202 {
		t.Fatalf("faucet: %d", code)
	}
	if code, body := call(t, h, "POST", "/mine", ""); code != 201 {
		t.Fatalf("mine: %d %v", code, body)
	}
	if code, _ := call(t, h, "POST", "/transactions", `{"from":"alice","to":"bob","amount":30}`); code != 202 {
		t.Fatalf("transfer: %d", code)
	}
	if code, _ := call(t, h, "POST", "/mine", ""); code != 201 {
		t.Fatalf("second mine: %d", code)
	}

	code, body := call(t, h, "GET", "/balances", "")
	if code != 200 {
		t.Fatalf("balances: %d", code)
	}
	if total, _ := body["total_supply"].(float64); total != 200 {
		t.Errorf("total supply = %v, want 200 (100 faucet + two 50 rewards)", body["total_supply"])
	}

	if code, body := call(t, h, "GET", "/balances/alice", ""); code != 200 || body["balance"].(float64) != 70 {
		t.Errorf("alice = %v, want 70", body["balance"])
	}
	if code, body := call(t, h, "GET", "/chain/validate", ""); code != 200 || body["valid"] != true {
		t.Errorf("validate: %d %v", code, body)
	}
	if code, body := call(t, h, "GET", "/chain", ""); code != 200 || body["height"].(float64) != 2 {
		t.Errorf("chain height = %v, want 2", body["height"])
	}
	if code, body := call(t, h, "GET", "/blocks/1", ""); code != 200 || body["height"].(float64) != 1 {
		t.Errorf("block 1: %d %v", code, body)
	}
}

// Every documented failure must answer with the right status and a useful
// message, since that is what a Postman user sees first.
func TestErrorResponses(t *testing.T) {
	h := newTestServer(t)
	if code, _ := call(t, h, "POST", "/keys", `{"label":"alice"}`); code != 201 {
		t.Fatal("setup: creating alice")
	}
	if code, _ := call(t, h, "POST", "/faucet", `{"to":"alice","amount":100}`); code != 202 {
		t.Fatal("setup: faucet")
	}
	if code, _ := call(t, h, "POST", "/mine", ""); code != 201 {
		t.Fatal("setup: mine")
	}

	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"duplicate key label", "POST", "/keys", `{"label":"alice"}`, http.StatusConflict},
		{"unknown sender", "POST", "/transactions", `{"from":"carol","to":"alice","amount":1}`, http.StatusNotFound},
		{"overspend", "POST", "/transactions", `{"from":"alice","to":"bob","amount":99999}`, http.StatusBadRequest},
		{"non-positive amount", "POST", "/faucet", `{"to":"alice","amount":0}`, http.StatusBadRequest},
		{"malformed JSON", "POST", "/faucet", `{"to":`, http.StatusBadRequest},
		{"unknown field", "POST", "/faucet", `{"to":"alice","amount":1,"fee":2}`, http.StatusBadRequest},
		{"block out of range", "GET", "/blocks/99", "", http.StatusNotFound},
		{"block height not a number", "GET", "/blocks/abc", "", http.StatusBadRequest},
		{"wrong method", "GET", "/mine", "", http.StatusMethodNotAllowed},
		{"unknown route", "GET", "/nope", "", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, h, tc.method, tc.path, tc.body)
			if code != tc.want {
				t.Errorf("got %d, want %d (%v)", code, tc.want, body)
			}
			// Errors this API produces itself must explain themselves; the two
			// routing failures are answered by net/http with an empty body.
			if code >= 400 && body != nil && body["error"] == nil && body["valid"] == nil {
				t.Errorf("error response carries no message: %v", body)
			}
		})
	}
}

// An unknown account is a legitimate question with a legitimate answer: zero.
func TestUnknownAccountReadsZero(t *testing.T) {
	h := newTestServer(t)
	code, body := call(t, h, "GET", "/balances/nobody", "")
	if code != 200 || body["balance"].(float64) != 0 {
		t.Errorf("unknown account: %d %v", code, body)
	}
}

// The private key must never leave the keystore, whatever the endpoint.
func TestPrivateKeysAreNeverReturned(t *testing.T) {
	h := newTestServer(t)
	if code, _ := call(t, h, "POST", "/keys", `{"label":"alice"}`); code != 201 {
		t.Fatal("setup")
	}
	for _, path := range []string{"/keys", "/balances"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		for _, leak := range []string{"private", "privkey", "secret"} {
			if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
				t.Errorf("%s response mentions %q:\n%s", path, leak, rec.Body.String())
			}
		}
	}
}
