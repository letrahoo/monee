package ledger

import "testing"

// A refund may only return money to a fund account the ledger already uses;
// a doubled or mistyped name must be rejected rather than create an account.
func TestRefundAccountMustAlreadyExist(t *testing.T) {
	s := testStore(t)
	a := testInput()
	a.Amount, a.Account = "25", "合成钱包A"
	original, err := s.Create(a, "refund-account-original-a")
	if err != nil {
		t.Fatal(err)
	}
	b := testInput()
	b.Amount, b.Account, b.Merchant = "8", "合成钱包B", "另一商户"
	if _, err = s.Create(b, "refund-account-original-b"); err != nil {
		t.Fatal(err)
	}
	summary, err := s.Refunds(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, option := range summary.Accounts {
		if option.ID == "" {
			t.Fatal("account option without id", summary.Accounts)
		}
		names = append(names, option.Name)
	}
	if len(names) != 2 || names[0] != "合成钱包A" || names[1] != "合成钱包B" {
		t.Fatal("refund account options", names)
	}
	before, err := s.version()
	if err != nil {
		t.Fatal(err)
	}
	var accounts int
	countAccounts := func() int {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE ledger_id=?", s.ledgerID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	accounts = countAccounts()
	for _, name := range []string{"合成钱包B合成钱包B", "合成钱包", "餐饮", "不存在的账户"} {
		in := RefundInput{Version: original.Version, Date: "2026-09-20", Amount: "5", Currency: "CNY", Account: name}
		if _, err = s.CreateRefund(original.ID, in, "refund-account-unknown-001"); err == nil {
			t.Fatal("accepted unknown refund account", name)
		}
	}
	after, err := s.version()
	if err != nil {
		t.Fatal(err)
	}
	if after != before || countAccounts() != accounts {
		t.Fatal("rejected refund mutated ledger", before, after, accounts, countAccounts())
	}
	if summary, err = s.Refunds(original.ID); err != nil || summary.RefundedMinor != "0" {
		t.Fatal(summary, err)
	}
	in := RefundInput{Version: original.Version, Date: "2026-09-20", Amount: "5", Currency: "CNY", Account: "合成钱包B"}
	first, err := s.CreateRefund(original.ID, in, "refund-account-known-001")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateRefund(original.ID, in, "refund-account-known-001")
	if err != nil || retry.ID != first.ID || first.Account != "合成钱包B" {
		t.Fatal("known account refund/retry", first, retry, err)
	}
	if countAccounts() != accounts {
		t.Fatal("known account refund created an account")
	}
}
