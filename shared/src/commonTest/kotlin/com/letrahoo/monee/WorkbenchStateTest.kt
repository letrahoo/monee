package com.letrahoo.monee

import com.letrahoo.monee.data.DashboardFilters
import kotlin.test.*

class WorkbenchStateTest {
    @Test fun monthsRespectBoundariesAndNeverUseClientWallClock() {
        assertEquals("2025-12", adjacentWorkbenchMonth("2026-01", -1))
        assertEquals("2027-01", adjacentWorkbenchMonth("2026-12", 1))
        assertNull(adjacentWorkbenchMonth("1900-01", -1))
        assertNull(adjacentWorkbenchMonth("2200-12", 1))
        for (bad in listOf("", "1899-12", "2201-01", "2026-00", "2026-13", "2026-1")) {
            assertFalse(validWorkbenchMonth(bad))
            assertNull(adjacentWorkbenchMonth(bad, 1))
        }
    }

    @Test fun monthAndTabNavigationRetainsFiltersButResetsPagination() {
        val state = LedgerViewState()
        state.filter(DashboardFilters("refund", "synthetic-account", "餐饮"))
        state.search("synthetic query")
        state.page = 3
        state.monthPanel = true
        state.selectMonth("2026-09")
        assertEquals(1, state.page)
        assertFalse(state.monthPanel)
        state.selectTab(WorkbenchTab.Statistics)
        state.statistics.showIncome=false
        state.statistics.expanded=true
        state.selectTab(WorkbenchTab.Details)
        assertFalse(state.statistics.showIncome)
        assertTrue(state.statistics.expanded)
        assertEquals("2026-09", state.month)
        assertEquals("synthetic query", state.query)
        assertEquals("refund", state.filters.type)
        assertEquals(4, state.scrolls.size)
        assertNotSame(state.scrolls[WorkbenchTab.Details], state.scrolls[WorkbenchTab.Statistics])
        state.clearFilters()
        assertEquals(DashboardFilters(), state.filters)
        assertEquals("", state.query)
        assertEquals(1, state.page)
        assertFailsWith<IllegalArgumentException> { state.selectMonth("2026-13") }
    }

    @Test fun independentLedgerPreferencesNeverShareSelectedFilters() {
        val a = LedgerViewState()
        val b = LedgerViewState()
        a.selectMonth("2025-02")
        a.filter(DashboardFilters(category="餐饮"))
        a.selectTab(WorkbenchTab.Mine)
        assertEquals("", b.month)
        assertEquals(DashboardFilters(), b.filters)
        assertEquals(WorkbenchTab.Overview, b.tab)
        assertEquals(listOf("总览", "明细", "统计", "我的"), WorkbenchTab.entries.map { it.label })
    }

    @Test fun yearGridAlwaysContainsSelectedYearAndUsesAtMostTwelveChoices() {
        for (year in 1900..2200) {
            val grid = yearGrid(yearGridStart(year))
            assertTrue(year in grid)
            assertTrue(grid.size in 1..12)
            assertTrue(grid.all { it in 1900..2200 })
        }
        assertEquals(12, yearGrid(1900).size)
        assertEquals(listOf(2200), yearGrid(2200))
    }
}
