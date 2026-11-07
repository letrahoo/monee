package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlin.test.*

/** Synthetic DTOs only: no real service, persisted credentials, or ledger fixtures. */
class WorkbenchPanelsTest {
    private fun month(key: String, records: Boolean = true, income: String = "0", expense: String = "0") =
        YearMonthSummary(key, records, if (records) 1 else 0, income, "0", "0", expense, "0")

    private fun report(months: List<YearMonthSummary> = emptyList()) = AnnualReport(
        ledgerId = "synthetic", version = 1, year = 2026, today = "2026-09-19", years = listOf(2026),
        incomeMinor = "0", grossExpenseMinor = "0", refundMinor = "0", expenseMinor = "0", surplusMinor = "0",
        totalCount = 0, months = months,
    )

    @Test fun moneyIsExactWithoutLongOrFloatingPointRoundTrips() {
        assertEquals("¥0.00", workbenchMoney("0"))
        assertEquals("¥0.01", workbenchMoney("1"))
        assertEquals("¥0.09", workbenchMoney("0009"))
        assertEquals("¥0.00", workbenchMoney("-000"))
        assertEquals("−¥12.34", workbenchMoney("-1234"))
        assertEquals("−¥92,233,720,368,547,758.08", workbenchMoney(Long.MIN_VALUE.toString()))
        assertEquals("¥12,345,678,901,234,567,890,123,456,789.01", workbenchMoney("1234567890123456789012345678901"))
        assertEquals("金额不可用", workbenchMoney("12.34"))
        assertEquals("金额不可用", workbenchMoney(""))
        assertEquals("金额不可用", workbenchMoney("1e9"))
    }

    @Test fun grossPercentUsesIntegerRatioWithZeroNegativeAndOverflowBoundaries() {
        assertEquals("0.0%", grossShareLabel("0", "0"))
        assertEquals("0.0%", grossShareLabel("42", "0"))
        assertEquals("0.0%", grossShareLabel("1", "-1"))
        assertEquals("0.0%", grossShareLabel("-1", "100"))
        assertEquals("0.0%", grossShareLabel("bad", "100"))
        assertEquals("50.0%", grossShareLabel("005", "0010"))
        assertEquals("33.3%", grossShareLabel("1", "3"))
        assertEquals("0.0%", grossShareLabel("1", "1001"))
        assertEquals("100.0%", grossShareLabel("200", "100"))
        assertEquals(500, grossSharePermille("999999999999999999999999999999", "1999999999999999999999999999998"))
        assertEquals(1000, grossSharePermille("9223372036854775807", "9223372036854775807"))
    }

    @Test fun categoryTitleUsesServerGrossRatherThanRefundReducedNet() {
        val category = CategoryTotal("合成分类", amountMinor = "-200", grossExpenseMinor = "100", refundMinor = "300")
        assertEquals("合成分类 · ¥1.00 · 50.0%", workbenchGrossCategoryLabel(category, "200"))
        assertEquals("-200", category.amountMinor)
        assertEquals("300", category.refundMinor)
    }

    @Test fun strictMonthLabelsAndAdjacentMonthsDoNotCrossSupportedYears() {
        assertEquals("2026年1月", workbenchMonthLabel("2026-01"))
        assertEquals("2200年12月", workbenchMonthLabel("2200-12"))
        listOf("1899-12", "2201-01", "2026-00", "2026-13", "2026-1", "2026-01-01", "").forEach {
            assertFalse(validWorkbenchMonth(it), it)
            assertEquals("月份不可用", workbenchMonthLabel(it))
            assertNull(adjacentWorkbenchMonth(it, 1))
        }
        assertNull(adjacentWorkbenchMonth("1900-01", -1))
        assertNull(adjacentWorkbenchMonth("2200-12", 1))
        assertNull(adjacentWorkbenchMonth("2026-01", 2))
        assertEquals("2025-12", adjacentWorkbenchMonth("2026-01", -1))
        assertEquals("2027-01", adjacentWorkbenchMonth("2026-12", 1))
        assertEquals("2026-01", adjacentWorkbenchMonth("2026-01", 0))
    }

    @Test fun yearGridUsesTwelveYearPagesAndOnlySupportedYears() {
        assertEquals(1900, yearGridStart(1900))
        assertEquals(1900, yearGridStart(1911))
        assertEquals(1912, yearGridStart(1912))
        assertEquals(2020, yearGridStart(2026))
        assertEquals((2020..2031).toList(), yearGrid(yearGridStart(2026)))
        assertEquals((2188..2199).toList(), yearGrid(yearGridStart(2199)))
        assertEquals(listOf(2200), yearGrid(yearGridStart(2200)))
        assertEquals(1900, yearGridStart(1800))
        assertEquals(2200, yearGridStart(2300))
        val pages = (1900..2200 step 12).flatMap(::yearGrid)
        assertEquals((1900..2200).toList(), pages)
    }

    @Test fun calendarSlotsDoNotClaimMissingDataOrContinuousCoverage() {
        val january = month("2026-01", income = "123", expense = "-456")
        val marchNoRecords = month("2026-03", records = false)
        val aprilZeroRecords = month("2026-04")
        val data = report(listOf(aprilZeroRecords, month("2025-02"), marchNoRecords, january))
        val slots = workbenchYearSlots(data)
        assertEquals(12, slots.size)
        assertSame(january, slots[0])
        assertNull(slots[1])
        assertSame(marchNoRecords, slots[2])
        assertEquals("数据未提供", workbenchMonthStatus(slots[1]))
        assertEquals("无记录", workbenchMonthStatus(slots[2]))
        assertEquals("1笔", workbenchMonthStatus(slots[3]))
        assertEquals(listOf("123", null, null, "0") + List(8) { null }, workbenchTrendValues(data, true))
        assertEquals(listOf("-456", null, null, "0") + List(8) { null }, workbenchTrendValues(data, false))
    }

    @Test fun trendScaleTracksVisibleSeriesAndZeroOrEmptyYear() {
        val data = report(listOf(month("2026-01", income = "999999999999999999999", expense = "-1200")))
        assertEquals("999999999999999999999", workbenchTrendMagnitude(workbenchTrendValues(data, true)))
        assertEquals("1200", workbenchTrendMagnitude(workbenchTrendValues(data, false)))
        assertEquals("0", workbenchTrendMagnitude(workbenchTrendValues(report(), true)))
        assertEquals("0", workbenchTrendMagnitude(workbenchTrendValues(report(listOf(month("2026-01"))), false)))
        assertEquals(List(12) { null }, workbenchTrendValues(report(listOf(month("2026-01", income = "bad"))), true))
    }

    @Test fun annualDtoKeepsAllServerFinancialStringsAndLegacyCategoryDefaults() {
        val data = apiJson.decodeFromString<AnnualReport>("""{
          "ledgerId":"synthetic","version":1,"year":2026,"today":"2026-09-19","years":[2025,2026],
          "incomeMinor":"9007199254740993","grossExpenseMinor":"100","refundMinor":"300",
          "expenseMinor":"-200","surplusMinor":"9007199254741193","totalCount":2,
          "months":[{"month":"2026-09","hasRecords":true,"totalCount":2,"incomeMinor":"9007199254740993",
          "grossExpenseMinor":"100","refundMinor":"300","expenseMinor":"-200","surplusMinor":"9007199254741193"}],
          "categories":[{"name":"合成分类","amountMinor":"-200","grossExpenseMinor":"100","refundMinor":"300"}]
        }""")
        assertEquals("9007199254740993", data.incomeMinor)
        assertEquals("9007199254741193", data.surplusMinor)
        assertEquals("-200", data.months.single().expenseMinor)
        assertTrue(data.months.single().categories.isEmpty())
        assertEquals("100.0%", grossShareLabel(data.categories.single().grossExpenseMinor, data.grossExpenseMinor))
        assertEquals("−¥2.00", workbenchMoney(data.categories.single().amountMinor))
        val old = apiJson.decodeFromString<CategoryTotal>("""{"name":"合成旧分类","amountMinor":"123"}""")
        assertEquals("123", old.grossExpenseMinor)
        assertEquals("0", old.refundMinor)
    }

    @Test fun filterDtoKeepsStableIdsAndExactCategoryWhileTransferRemainsValid() {
        val options = apiJson.decodeFromString<DashboardFilterOptions>("""{
          "accounts":[{"id":"account-stable-a","name":"同名合成账户"},{"id":"account-stable-b","name":"同名合成账户"}],
          "categories":["交通 / 通勤","交通"]
        }""")
        val chosen = DashboardFilters(type = "transfer", accountId = options.accounts[1].id, category = options.categories[0])
        assertEquals("account-stable-b", chosen.accountId)
        assertEquals("交通 / 通勤", chosen.category)
        assertEquals("转账", workbenchFilterTypes.single { it.first == chosen.type }.second)
        assertEquals(listOf("", "income", "expense", "refund", "transfer"), workbenchFilterTypes.map { it.first })
        assertTrue(chosen.active)
        assertEquals(chosen.accountId, chosen.copy(type = "refund").accountId)
        assertFalse(DashboardFilters().active)
        assertTrue(apiJson.decodeFromString<DashboardFilterOptions>("{}").accounts.isEmpty())
        assertTrue(apiJson.decodeFromString<DashboardFilterOptions>("{}").categories.isEmpty())
    }
}
