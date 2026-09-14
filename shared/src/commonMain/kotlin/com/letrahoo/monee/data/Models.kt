package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

@Serializable
data class LedgerTransaction(
    val id: String, val date: String, val type: String, val amountMinor: String,
    val currency: String, val merchant: String, val category: String, val source: String,
    val account: String, val externalId: String, val note: String, val version: Long,
)

@Serializable
data class TransactionInput(
    val date: String = "", val type: String = "expense", val amount: String = "",
    val currency: String = "CNY", val merchant: String = "", val category: String = "",
    val source: String = "手动记账", val account: String = "", val externalId: String = "", val note: String = "",
)

@Serializable data class CategoryTotal(val name: String, val amountMinor: String)
@Serializable
data class Dashboard(
    val ledgerId: String, val version: Long, val month: String, val today: String,
    val months: List<String>, val incomeMinor: String, val expenseMinor: String,
    val sourceCount: Int, val totalCount: Int, val filteredCount: Int,
    val page: Int, val pageSize: Int, val categories: List<CategoryTotal>,
    val transactions: List<LedgerTransaction>,
)

@Serializable data class ImportRow(val line: Int, val record: LedgerTransaction, val status: String, val message: String)
@Serializable
data class ImportPreview(
    val id: String, val filename: String, val ledgerVersion: Long, val rows: List<ImportRow>,
    val newCount: Int, val duplicateCount: Int, val similarCount: Int,
    val errors: List<String>, val alreadyCommitted: Boolean,
)
@Serializable data class ImportRequest(val filename: String, val csv: String)
@Serializable data class CommitRequest(val ledgerVersion: Long, val confirmSimilar: Boolean)
@Serializable data class CommitResult(val importId: String, val added: Int, val skipped: Int, val month: String)
@Serializable data class Connection(val baseUrl: String, val token: String)
@Serializable data class PickedCSV(val name: String, val text: String)
@Serializable data class APIProblem(val code: String = "error", val message: String = "请求失败")
