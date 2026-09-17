package ingestion

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ParseWeChat chooses a container by its bytes, never by an untrusted filename.
// Both formats must still pass their own structure and required-column checks.
func ParseWeChat(raw []byte, account string) Document {
	if bytes.HasPrefix(raw, []byte("PK")) {
		return ParseWeChatXLSX(raw, account)
	}
	return ParseWeChatCSV(raw, account)
}

var weChatColumns = []string{"交易时间", "交易类型", "交易对方", "商品", "收/支", "金额(元)", "支付方式", "当前状态", "交易单号", "商户单号"}

func weChatRecord(fields map[string]string, account string, ordinal, line int) Record {
	get := func(k string) string { return strings.TrimSpace(fields[k]) }
	amount := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(get("金额(元)"), "¥"), "￥"))
	f := Fields{Date: get("交易时间"), Type: get("收/支"), Amount: amount, Currency: "CNY", Merchant: get("交易对方"), Category: get("交易类型"), Source: "微信", Account: strings.TrimSpace(account), ExternalID: get("交易单号"), Note: get("商品")}
	scope, _ := json.Marshal([]string{"wechat", f.Account})
	ids := []Identifier{}
	if v := f.ExternalID; v != "" && v != "/" && v != "-" {
		ids = append(ids, Identifier{"wechat_payment_trace", string(scope), v})
	}
	if v := get("商户单号"); v != "" && v != "/" && v != "-" {
		scope, _ := json.Marshal([]string{"wechat", f.Account, f.Merchant})
		ids = append(ids, Identifier{"merchant_order", string(scope), v})
	}
	return Record{Ordinal: ordinal, Line: line, Fields: f, Identifiers: ids, Raw: fields}
}
