package ledger

// This adapter prepares a reviewable import bundle. It never writes a ledger or
// treats the legacy dashboard's spending convention as Monee accounting rules.
import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type legacyRecord struct {
	ID          string      `json:"transactionId"`
	Time        string      `json:"transactionTime"`
	Source      string      `json:"source"`
	Account     string      `json:"accountName"`
	Direction   string      `json:"direction"`
	Amount      json.Number `json:"amount"`
	Currency    string      `json:"currency"`
	Merchant    string      `json:"merchant"`
	Description string      `json:"description"`
	Order       string      `json:"orderId"`
	Category    string      `json:"category"`
	Status      string      `json:"status"`
	Notes       string      `json:"notes"`
	Included    bool        `json:"includedInSpend"`
	Disposition string      `json:"disposition"`
	Excluded    bool        `json:"isExcluded"`
	Canonical   bool        `json:"isCanonical"`
	Lineage     struct {
		RawID    string `json:"rawRecordId"`
		BatchID  string `json:"batchId"`
		FileHash string `json:"rawFileSha256"`
	} `json:"lineage"`
}

type LegacyDecision struct {
	ID          string `json:"legacyId"`
	Disposition string `json:"legacyDisposition"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	AmountMinor string `json:"amountMinor,omitempty"`
}

type LegacyFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Rows   int    `json:"rows"`
	CSV    string `json:"-"`
}

type LegacyPlan struct {
	Version               int              `json:"version"`
	SourceSHA256          string           `json:"sourceSha256"`
	GeneratedAt           string           `json:"legacyGeneratedAt"`
	Total                 int              `json:"total"`
	Candidates            int              `json:"candidates"`
	Review                int              `json:"review"`
	Archived              int              `json:"archived"`
	CandidateExpenseMinor string           `json:"candidateExpenseMinor"`
	Reasons               map[string]int   `json:"reasons"`
	Files                 []LegacyFile     `json:"files"`
	Decisions             []LegacyDecision `json:"decisions"`
}

func legacyText(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimLeft(strings.TrimPrefix(s, "退款"), "-— ")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// PrepareLegacy accepts the deployed product-snapshot schema 1.0, retaining all
// other snapshot fields verbatim in the CLI's source archive. Amounts are parsed
// from JSON number tokens, never float64. No rounding repairs are performed.
func PrepareLegacy(raw []byte) (LegacyPlan, error) {
	p := LegacyPlan{Version: 1, SourceSHA256: hash(string(raw)), Reasons: map[string]int{}, Files: []LegacyFile{}, Decisions: []LegacyDecision{}}
	var snapshot struct {
		Schema      string `json:"schemaVersion"`
		GeneratedAt string `json:"generatedAt"`
		Audit       struct {
			Total int            `json:"totalTransactions"`
			Rows  []legacyRecord `json:"transactions"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return p, fmt.Errorf("invalid legacy snapshot: %w", err)
	}
	if snapshot.Schema != "1.0" {
		return p, fmt.Errorf("unsupported legacy schema %q", snapshot.Schema)
	}
	if len(snapshot.Audit.Rows) == 0 || snapshot.Audit.Total != len(snapshot.Audit.Rows) {
		return p, fmt.Errorf("legacy audit count mismatch or empty audit")
	}
	p.GeneratedAt = snapshot.GeneratedAt
	p.Total = snapshot.Audit.Total
	rows := snapshot.Audit.Rows
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Time == rows[j].Time {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Time < rows[j].Time
	})
	ids := map[string]bool{}
	refunds := []legacyRecord{}
	for _, r := range rows {
		if r.ID == "" || ids[r.ID] {
			return p, fmt.Errorf("missing or repeated legacy transaction ID")
		}
		ids[r.ID] = true
		if r.Direction == "refund" {
			refunds = append(refunds, r)
		}
	}
	var candidates []Input
	var total int64
	for _, r := range rows {
		d := LegacyDecision{ID: r.ID, Disposition: r.Disposition, Decision: "review"}
		minor, amountErr := parseAmount(r.Amount.String())
		if amountErr == nil {
			d.AmountMinor = strconv.FormatInt(minor, 10)
		}
		switch {
		case !r.Canonical || r.Excluded || r.Disposition == "duplicate" || r.Disposition == "excluded_source" || r.Disposition == "excluded" || r.Disposition == "fully_refunded_original":
			d.Decision, d.Reason = "archive", "legacy_excluded"
		case r.Direction != "expense":
			d.Reason = "unsupported_or_unverified_direction"
		case !r.Included || r.Disposition != "included_spend":
			d.Reason = "not_in_legacy_spend"
		case r.Currency != "CNY" || amountErr != nil:
			d.Reason = "currency_or_amount"
		case r.Lineage.RawID == "" || r.Lineage.BatchID == "" || r.Lineage.FileHash == "":
			d.Reason = "missing_lineage"
		default:
			combined := r.Status + " " + r.Description + " " + r.Notes
			for _, word := range []string{"退款", "还款", "转账", "提现", "充值", "冻结", "理财", "赎回", "交易关闭", "失败"} {
				if strings.Contains(combined, word) {
					d.Reason = "special_transaction_hint"
					break
				}
			}
			if d.Reason == "" {
				for _, refund := range refunds {
					// Conservative review only: this is not an asserted refund pairing.
					sameOrder := r.Order != "" && refund.Order != "" && (strings.HasPrefix(refund.Order, r.Order) || strings.HasPrefix(r.Order, refund.Order))
					sameMerchant := legacyText(r.Merchant) != "" && legacyText(r.Merchant) == legacyText(refund.Merchant)
					sameDescription := legacyText(r.Description) != "" && legacyText(r.Description) == legacyText(refund.Description)
					if sameOrder || sameMerchant || sameDescription {
						d.Reason = "possible_refund_relation"
						break
					}
				}
			}
			if d.Reason == "" {
				date := r.Time
				if len(date) >= 10 {
					date = date[:10]
				}
				note := "ai-financial:" + r.ID + "; raw:" + r.Lineage.RawID + "; batch:" + r.Lineage.BatchID
				if r.Order != "" {
					note += "; order:" + r.Order
				}
				if r.Description != "" {
					note += "\n" + r.Description
				}
				if r.Notes != "" {
					note += "\n" + r.Notes
				}
				// A stable namespaced ID prevents reruns from duplicating legacy records.
				// Future native-format overlap still needs the normal similarity review.
				in := Input{Date: date, Type: "expense", Amount: r.Amount.String(), Currency: r.Currency, Merchant: r.Merchant, Category: r.Category, Source: r.Source, Account: r.Account, ExternalID: "ai-financial:" + r.ID, Note: note}
				if _, err := normalize(in); err != nil {
					d.Reason = "current_ledger_validation"
				} else {
					candidates = append(candidates, in)
					total += minor
					d.Decision, d.Reason = "candidate", "ordinary_expense"
				}
			}
		}
		p.Reasons[d.Reason]++
		switch d.Decision {
		case "candidate":
			p.Candidates++
		case "archive":
			p.Archived++
		default:
			p.Review++
		}
		p.Decisions = append(p.Decisions, d)
	}
	p.CandidateExpenseMinor = strconv.FormatInt(total, 10)
	// Leave headroom for JSON escaping in the 3 MiB HTTP request envelope.
	// Split on row count and a conservative 1 MiB encoded CSV byte limit.
	var batch []Input
	headerBytes := len(legacyCSV(nil))
	batchBytes := headerBytes
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		data := legacyCSV(batch)
		_, issues := parseCSV(data)
		if len(issues) > 0 {
			return fmt.Errorf("generated CSV failed validation: %s", strings.Join(issues, "; "))
		}
		p.Files = append(p.Files, LegacyFile{Name: fmt.Sprintf("candidates-%03d.csv", len(p.Files)+1), SHA256: hash(data), Rows: len(batch), CSV: data})
		batch = nil
		batchBytes = headerBytes
		return nil
	}
	for _, in := range candidates {
		rowBytes := len(legacyCSV([]Input{in})) - headerBytes
		if len(batch) == MaxRows || batchBytes+rowBytes > MaxCSVBytes/2 {
			if err := flush(); err != nil {
				return p, err
			}
		}
		batch = append(batch, in)
		batchBytes += rowBytes
	}
	if err := flush(); err != nil {
		return p, err
	}
	return p, nil
}

func legacyCSV(rows []Input) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write(headers)
	for _, r := range rows {
		_ = w.Write([]string{r.Date, r.Type, r.Amount, r.Currency, r.Merchant, r.Category, r.Source, r.Account, r.ExternalID, r.Note})
	}
	w.Flush()
	return b.String()
}
