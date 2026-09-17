package ledger

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/letrahoo/monee/server/internal/ingestion"
)

// PreviewAlipay preserves the original bytes in the existing private imports
// envelope. It does not convert the file to standard CSV or discard review rows.
func (s *Store) PreviewAlipay(filename, content, account string) (Preview, error) {
	return s.previewNative("alipay", filename, content, account)
}

func (s *Store) PreviewWeChat(filename, content, account string) (Preview, error) {
	return s.previewNative("wechat", filename, content, account)
}

func (s *Store) previewNative(format, filename, content, account string) (Preview, error) {
	if len(content) > ((ingestion.MaxBytes+2)/3)*4 {
		return Preview{}, problem("invalid", "文件不能超过 2 MiB")
	}
	account = strings.TrimSpace(account)
	if account == "" || len([]rune(account)) > 80 {
		return Preview{}, problem("invalid", "请填写 1 至 80 字的固定来源账户名称")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(content)
	if err != nil {
		return Preview{}, problem("invalid", "文件编码无效，请重新选择文件")
	}
	var d ingestion.Document
	if format == "wechat" {
		d = ingestion.ParseWeChat(raw, account)
	} else {
		d = ingestion.ParseAlipayCSV(raw, account)
	}
	// Stable serialization includes source account; identical bytes uploaded for
	// different wallet accounts must not reuse a batch or payment identity.
	envelopeFormat := "alipay-csv-v1"
	if format == "wechat" {
		envelopeFormat = d.Parser + "-v1"
	}
	envelope := encode(struct{ Format, Account, Content string }{envelopeFormat, account, base64.StdEncoding.EncodeToString(raw)})
	return s.preview(filename, envelope, &d)
}

func (s *Store) nativeRows(d ingestion.Document) ([]PreviewRow, []PendingRow) {
	rows := []PreviewRow{}
	pending := []PendingRow{}
	// A refund may arrive as a separate row or as a changed original status.
	// Without an allocation model, keep same-merchant originals out of booking
	// whenever this document contains a refund. This deliberately favors review.
	refundMerchants := map[string]bool{}
	for _, r := range d.Records {
		if strings.Contains(r.Raw["交易状态"]+r.Raw["当前状态"], "退") || strings.Contains(r.Fields.Category, "退款") {
			refundMerchants[r.Fields.Merchant] = true
		}
	}
	for _, r := range d.Records {
		reason := ""
		f := r.Fields
		status := strings.TrimSpace(r.Raw["交易状态"])
		success := status == "交易成功"
		if f.Source == "微信" {
			status = strings.TrimSpace(r.Raw["当前状态"])
			success = status == "支付成功" && f.Category == "商户消费"
		}
		if !success {
			reason = "交易状态需核实：" + status
		}
		if refundMerchants[f.Merchant] {
			reason = "该商户存在退款记录，原交易与退款需关联核实"
		}
		business := f.Category + " " + f.Note + " " + f.Merchant
		for _, word := range []string{"退款", "转账", "还款", "充值", "提现", "理财", "基金", "借款", "借呗", "余额宝", "红包", "代付", "分期", "外币", "美元", "港币", "转入", "转出", "信用借还", "借还", "收款", "金融", "利息", "手续费"} {
			if strings.Contains(business, word) {
				reason = "涉及" + word + "，暂不作为普通收支入账"
				break
			}
		}
		kind := ""
		switch f.Type {
		case "支出":
			kind = "expense"
		case "收入":
			kind = "income"
		default:
			reason = "收支性质需核实：" + f.Type
		}
		if kind == "income" && f.Category != "工资" && f.Category != "工资收入" {
			reason = "收入来源需核实，避免把退款或转入计为收入"
		}
		if kind == "expense" && f.Source != "微信" {
			allowed := map[string]bool{"餐饮美食": true, "日用百货": true, "交通出行": true, "服饰装扮": true, "数码电器": true, "家居家装": true, "运动户外": true, "美容美发": true, "母婴亲子": true, "宠物": true, "文化休闲": true, "生活服务": true, "教育培训": true, "医疗健康": true, "酒店旅游": true, "住房物业": true, "商业服务": true, "公益捐赠": true}
			if !allowed[f.Category] {
				reason = "交易分类尚不能确定为普通消费：" + f.Category
			}
		}
		var key string
		for _, id := range r.Identifiers {
			if (f.Source == "支付宝" && id.Kind == "alipay_payment_trace") || (f.Source == "微信" && id.Kind == "wechat_payment_trace") {
				key, _ = id.MatchKey(s.ledgerID)
			}
		}
		if key == "" {
			reason = "缺少支付交易流水号，不能以商家订单号替代"
		}
		date, err := time.Parse("2006-01-02 15:04:05", f.Date)
		if err != nil {
			reason = "交易时间格式无效"
		}
		prefix, payment, category := "alipay-native-v1:", r.Raw["收/付款方式"], f.Category
		if f.Source == "微信" {
			prefix, payment, category = "wechat-native-v1:", r.Raw["支付方式"], "未分类"
		}
		input := Input{Date: date.Format("2006-01-02"), Type: kind, Amount: f.Amount, Currency: f.Currency, Merchant: f.Merchant, Category: category, Source: f.Source, Account: strings.TrimSpace(payment), ExternalID: prefix + key, Note: f.Note}
		if input.Account == "/" || input.Account == "-" {
			input.Account = ""
		}
		normalized, validation := normalize(input)
		if validation != nil {
			reason = validation.Error()
		}
		if reason != "" {
			pending = append(pending, PendingRow{Line: r.Line, Date: f.Date, Merchant: f.Merchant, Amount: f.Amount, Reason: reason})
			continue
		}
		normalized.ID = newID()
		rows = append(rows, PreviewRow{Line: r.Line, Record: normalized, Status: "new"})
	}
	return rows, pending
}
