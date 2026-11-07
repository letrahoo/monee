package ledger

import (
	"reflect"
	"testing"
)

func TestMonthlyAndAnnualCategoriesUseGrossOrderDespiteRefunds(t *testing.T) {
	s := testStore(t)
	in := testInput()
	in.Date, in.Type, in.Amount, in.Category = "2026-09-14", "expense", "100", "高毛额低净额"
	original, err := s.Create(in, "gross-order-original-001")
	if err != nil {
		t.Fatal(err)
	}
	in.Amount, in.Category, in.Merchant = "80", "低毛额高净额", "另一合成商户"
	if _, err = s.Create(in, "gross-order-second-001"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRefund(original.ID, RefundInput{Version: original.Version, Date: original.Date, Amount: "90", Currency: "CNY", Account: original.Account}, "gross-order-refund-001"); err != nil {
		t.Fatal(err)
	}
	monthly, err := s.Dashboard("2026-09", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	annual, err := s.Annual("2026")
	if err != nil {
		t.Fatal(err)
	}
	if len(monthly.Categories) != 2 || monthly.Categories[0].Name != "高毛额低净额" || monthly.Categories[0].GrossExpenseMinor != "10000" || monthly.Categories[0].AmountMinor != "1000" {
		t.Fatal("monthly still sorted by net", monthly.Categories)
	}
	if !reflect.DeepEqual(monthly.Categories, annual.Categories) || !reflect.DeepEqual(monthly.Categories, annual.Months[8].Categories) {
		t.Fatal("month/year category order or amounts differ", monthly.Categories, annual.Categories)
	}
}
