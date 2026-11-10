package httpapi

import (
	"github.com/letrahoo/monee/server/internal/ledger"
	"net/http"
)

func (a API) registerExpensePairs(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/expense-pairs/preview", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.ExpensePairPreviewInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.PreviewExpensePair(in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/v1/expense-pairs", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in ledger.ExpensePairConfirmInput
		if !decode(w, r, &in) {
			return
		}
		out, err := s.ConfirmExpensePair(in, r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, out)
	})
	mux.HandleFunc("GET /api/v1/transactions/{id}/expense-pairs", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		out, err := s.ExpensePairs(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/v1/expense-pairs/{id}/withdraw", func(w http.ResponseWriter, r *http.Request) {
		s, ok := a.ledgerForRequest(w, r)
		if !ok {
			return
		}
		var in struct {
			LedgerVersion   int64  `json:"ledgerVersion"`
			DecisionVersion int64  `json:"decisionVersion"`
			Reason          string `json:"reason"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := s.WithdrawExpensePair(ledger.ExpensePairWithdrawInput{LedgerVersion: in.LedgerVersion, DecisionID: r.PathValue("id"), DecisionVersion: in.DecisionVersion, Reason: in.Reason}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, out)
	})
}
