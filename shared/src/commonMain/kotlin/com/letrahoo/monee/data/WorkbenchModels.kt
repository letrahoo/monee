package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

@Serializable data class FundingAccountOption(val id: String, val name: String)
@Serializable data class DashboardFilterOptions(
    val accounts: List<FundingAccountOption> = emptyList(),
    val categories: List<String> = emptyList(),
)
data class DashboardFilters(val type: String = "", val accountId: String = "", val category: String = "") {
    val active: Boolean get() = type.isNotEmpty() || accountId.isNotEmpty() || category.isNotEmpty()
}

@Serializable data class YearMonthSummary(
    val month: String, val hasRecords: Boolean, val totalCount: Int,
    val incomeMinor: String, val grossExpenseMinor: String, val refundMinor: String,
    val expenseMinor: String, val surplusMinor: String,
    val categories: List<CategoryTotal> = emptyList(),
)
@Serializable data class AnnualReport(
    val ledgerId: String, val version: Long, val year: Int, val today: String,
    val years: List<Int>, val incomeMinor: String, val grossExpenseMinor: String,
    val refundMinor: String, val expenseMinor: String, val surplusMinor: String,
    val totalCount: Int, val months: List<YearMonthSummary>,
    val categories: List<CategoryTotal> = emptyList(),
)
