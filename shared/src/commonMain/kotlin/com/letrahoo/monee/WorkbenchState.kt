package com.letrahoo.monee

import androidx.compose.foundation.ScrollState
import androidx.compose.runtime.*
import com.letrahoo.monee.data.DashboardFilters

internal enum class WorkbenchTab(val label: String) {
    Overview("总览"), Details("明细"), Statistics("统计"), Mine("我的")
}

/** User-scoped Workspace owns these per-ledger browsing preferences, never rows or credentials. */
internal class LedgerViewState {
    var tab by mutableStateOf(WorkbenchTab.Overview)
        private set
    var month by mutableStateOf("")
    var query by mutableStateOf("")
    var page by mutableStateOf(1)
    var filters by mutableStateOf(DashboardFilters())
    var monthPanel by mutableStateOf(false)
    var selectedId by mutableStateOf<String?>(null)
    val scrolls = WorkbenchTab.entries.associateWith { ScrollState(0) }
    val scrollOffsets = mutableMapOf<WorkbenchTab, Int>()
    val statistics = StatisticsPreferences()
    fun saveScroll() {scrollOffsets[tab]=scrolls.getValue(tab).value}
    fun selectTab(next:WorkbenchTab) {
        if(next!=tab){saveScroll();tab=next}
    }

    fun selectMonth(value: String) {
        require(validWorkbenchMonth(value))
        if (month != value) { month = value; page = 1 }
        monthPanel = false
    }
    fun filter(value: DashboardFilters) { filters = value; page = 1 }
    fun search(value: String) { query = value; page = 1 }
    fun clearFilters() { filters = DashboardFilters(); query = ""; page = 1 }
}

internal class StatisticsPreferences {
    var showIncome by mutableStateOf(true)
    var showExpense by mutableStateOf(true)
    var expanded by mutableStateOf(false)
}

internal data class DashboardRequest(val month:String,val query:String,val page:Int,val filters:DashboardFilters)

internal fun validWorkbenchMonth(value: String): Boolean =
    Regex("(?:19\\d{2}|20\\d{2}|21\\d{2}|2200)-(?:0[1-9]|1[0-2])").matches(value)

internal fun adjacentWorkbenchMonth(month: String, delta: Int): String? {
    if (!validWorkbenchMonth(month) || delta !in -1..1) return null
    val index = month.take(4).toInt() * 12 + month.takeLast(2).toInt() - 1 + delta
    val year = index / 12
    if (year !in 1900..2200) return null
    return "$year-${(index % 12 + 1).toString().padStart(2, '0')}"
}

internal fun yearGridStart(year: Int): Int = ((year.coerceIn(1900, 2200) - 1900) / 12) * 12 + 1900
internal fun yearGrid(start: Int): List<Int> = (start.coerceIn(1900, 2200)..minOf(start.coerceIn(1900, 2200) + 11, 2200)).toList()
