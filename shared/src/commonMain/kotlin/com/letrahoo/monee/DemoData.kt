package com.letrahoo.monee

data class PreviewTransaction(
    val id: String,
    val month: String,
    val date: String,
    val merchant: String,
    val category: String,
    val source: String,
    val amountMinor: Long,
    val note: String = "已整理",
)

// Synthetic presentation fixtures only. Production ledger/reporting rules belong to Go.
data class PreviewMonth(
    val key: String,
    val label: String,
    val expenseMinor: Long,
    val incomeMinor: Long,
    val insight: String,
)

val previewMonths = listOf(
    PreviewMonth("2026-09", "2026 年 9 月", 78250, 2500000, "本月演示账单中，居家支出占比最高。"),
    PreviewMonth("2026-08", "2026 年 8 月", 129900, 2500000, "本月演示账单中，一笔数码消费是主要支出。"),
)

val previewTransactions = listOf(
    PreviewTransaction("demo-001", "2026-09", "09-14", "街角咖啡", "餐饮", "支付宝", -2850),
    PreviewTransaction("demo-002", "2026-09", "09-13", "城市书店", "学习", "微信", -9600),
    PreviewTransaction("demo-003", "2026-09", "09-12", "家居用品", "居家", "京东白条", -65800),
    PreviewTransaction("demo-004", "2026-09", "09-10", "工资入账", "收入", "招商银行", 2500000),
    PreviewTransaction("demo-005", "2026-08", "08-24", "数码配件", "数码", "支付宝", -99900),
    PreviewTransaction("demo-006", "2026-08", "08-16", "朋友聚餐", "餐饮", "微信", -30000),
    PreviewTransaction("demo-007", "2026-08", "08-10", "工资入账", "收入", "招商银行", 2500000),
)

fun filterTransactions(month: String, query: String): List<PreviewTransaction> {
    val term = query.trim()
    return previewTransactions.filter { item ->
        item.month == month && listOf(item.merchant, item.category, item.source).any {
            it.contains(term, ignoreCase = true)
        }
    }
}

/** Formats integer minor units without floating point conversion, including Long.MIN_VALUE. */
fun formatMoney(minor: Long): String {
    val digits = minor.toString().removePrefix("-").padStart(3, '0')
    val integer = digits.dropLast(2).reversed().chunked(3).joinToString(",").reversed()
    return "${if (minor < 0) "−" else ""}¥$integer.${digits.takeLast(2)}"
}
