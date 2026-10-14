package com.letrahoo.monee

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.letrahoo.monee.data.*

/** Presentation only: preserve the API integer string, including values beyond Long. */
internal fun workbenchMoney(minor: String): String {
    if (!Regex("-?[0-9]+").matches(minor)) return "金额不可用"
    val magnitude = minor.removePrefix("-").trimStart('0').ifEmpty { "0" }
    val digits = magnitude.padStart(3, '0')
    val major = digits.dropLast(2).reversed().chunked(3).joinToString(",").reversed()
    return "${if (minor.startsWith('-') && magnitude != "0") "−" else ""}¥$major.${digits.takeLast(2)}"
}

private fun unsignedDigits(value: String): String? =
    if (Regex("[0-9]+").matches(value)) value.trimStart('0').ifEmpty { "0" } else null

private fun compareDigits(a: String, b: String): Int =
    if (a.length != b.length) a.length.compareTo(b.length) else a.compareTo(b)

private fun multiplyDigits(value: String, factor: Int): String {
    var carry = 0
    val reversed = StringBuilder()
    for (digit in value.reversed()) {
        val product = (digit - '0') * factor + carry
        reversed.append(product % 10)
        carry = product / 10
    }
    while (carry > 0) { reversed.append(carry % 10); carry /= 10 }
    return reversed.reverse().toString().trimStart('0').ifEmpty { "0" }
}

/** Integer ratio for display/geometry only. Never computes ledger totals or converts money to Float. */
internal fun grossSharePermille(gross: String, total: String): Int {
    val numerator = unsignedDigits(gross) ?: return 0
    val denominator = unsignedDigits(total) ?: return 0
    if (denominator == "0") return 0
    val scaled = multiplyDigits(numerator, 1000)
    var low = 0
    var high = 1000
    while (low < high) {
        val middle = (low + high + 1) / 2
        if (compareDigits(multiplyDigits(denominator, middle), scaled) <= 0) low = middle
        else high = middle - 1
    }
    return low
}

internal fun grossShareLabel(gross: String, total: String): String {
    val share = grossSharePermille(gross, total)
    return "${share / 10}.${share % 10}%"
}

internal fun workbenchMonthLabel(month: String): String =
    if (validWorkbenchMonth(month)) "${month.take(4)}年${month.takeLast(2).toInt()}月" else "月份不可用"

/** Always twelve calendar slots. Missing DTO rows remain unknown, not synthetic zero records. */
internal fun workbenchYearSlots(report: AnnualReport): List<YearMonthSummary?> = (1..12).map { number ->
    val key = "${report.year}-${number.toString().padStart(2, '0')}"
    report.months.firstOrNull { it.month == key }
}

internal fun workbenchMonthStatus(month: YearMonthSummary?): String = when {
    month == null -> "数据未提供"
    !month.hasRecords -> "无记录"
    else -> "${month.totalCount}笔"
}

internal val workbenchFilterTypes = listOf("" to "全部类型", "income" to "收入", "expense" to "支出", "refund" to "退款", "transfer" to "转账")

@Composable
private fun WorkbenchChoice(label: String, isSelected: Boolean, enabled: Boolean = true, modifier: Modifier = Modifier, onClick: () -> Unit) {
    OutlinedButton(
        onClick = onClick, enabled = enabled,
        modifier = modifier.sizeIn(minWidth = 48.dp, minHeight = 48.dp).semantics { selected = isSelected },
        colors = ButtonDefaults.outlinedButtonColors(
            backgroundColor = if (isSelected) Pine.copy(alpha = 0.12f) else Color.Transparent,
            contentColor = Ink,
        ),
    ) { Text(label) }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun WorkbenchNavigation(tab: WorkbenchTab, disabled: Boolean, onSelect: (WorkbenchTab) -> Unit) {
    FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        WorkbenchTab.entries.forEach { item ->
            WorkbenchChoice(item.label, item == tab, !disabled) { onSelect(item) }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun MonthToolbar(month: String, today: String, disabled: Boolean, onMonth: (String) -> Unit, onOpen: () -> Unit, monthFocus: FocusRequester? = null) {
    val previous = adjacentWorkbenchMonth(month, -1)
    val next = adjacentWorkbenchMonth(month, 1)
    val current = today.take(7).takeIf(::validWorkbenchMonth)
    FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        WorkbenchChoice("上个月", false, !disabled && previous != null) { previous?.let(onMonth) }
        WorkbenchChoice(workbenchMonthLabel(month), true, !disabled, modifier = monthFocus?.let { Modifier.focusRequester(it) } ?: Modifier) { onOpen() }
        WorkbenchChoice("下个月", false, !disabled && next != null) { next?.let(onMonth) }
        WorkbenchChoice("当前月", current == month, !disabled && current != null) { current?.let(onMonth) }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun MonthSelectionPanel(
    report: AnnualReport, selectedMonth: String, loading: Boolean,
    onMonth: (String) -> Unit, onYear: (Int) -> Unit, onClose: () -> Unit,
) {
    var gridStart by remember(report.year) { mutableStateOf(yearGridStart(report.year)) }
    val current = report.today.take(7).takeIf(::validWorkbenchMonth)
    Column(Modifier.fillMaxWidth().onPreviewKeyEvent { event ->
        if (event.key == Key.Escape && event.type == KeyEventType.KeyDown) { onClose(); true } else false
    }, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("选择月份 · ${report.year}年", color = Ink, style = MaterialTheme.typography.h6)
        if (loading) Text("正在加载年度数据…", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WorkbenchChoice("上一组年份", false, !loading && gridStart > 1900) { gridStart = maxOf(1900, gridStart - 12) }
            WorkbenchChoice("下一组年份", false, !loading && gridStart + 12 <= 2200) { gridStart += 12 }
            WorkbenchChoice("回到当前月", selectedMonth == current, !loading && current != null) { current?.let(onMonth) }
            WorkbenchChoice("收起月份选择", false, !loading) { onClose() }
        }
        Text("年份 ${gridStart}–${yearGrid(gridStart).last()}", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            yearGrid(gridStart).forEach { year -> WorkbenchChoice("${year}年", year == report.year, !loading) { onYear(year) } }
        }
        Text("全年收入 ${workbenchMoney(report.incomeMinor)} · 净支出 ${workbenchMoney(report.expenseMinor)}", color = Ink)
        Text("全年结余 ${workbenchMoney(report.surplusMinor)}（收入减净支出，非账户余额）", color = Ink)
        val slots = workbenchYearSlots(report)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            slots.forEachIndexed { index, summary ->
                val key = "${report.year}-${(index + 1).toString().padStart(2, '0')}"
                OutlinedButton(
                    onClick = { onMonth(key) }, enabled = !loading && validWorkbenchMonth(key),
                    modifier = Modifier.widthIn(min = 156.dp).heightIn(min = 96.dp).semantics { selected = key == selectedMonth },
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = Ink, backgroundColor = if (key == selectedMonth) Pine.copy(alpha = 0.12f) else Color.Transparent),
                ) {
                    Column {
                        Text("${index + 1}月 · ${workbenchMonthStatus(summary)}")
                        Text("收入 ${summary?.let { workbenchMoney(it.incomeMinor) } ?: "—"}")
                        Text("净支出 ${summary?.let { workbenchMoney(it.expenseMinor) } ?: "—"}")
                    }
                }
            }
        }
    }
}

/** Null slots are chart gaps; sign is retained even when refunds exceed that month's expense. */
internal fun workbenchTrendValues(report: AnnualReport, income: Boolean): List<String?> = workbenchYearSlots(report).map { month ->
    month?.takeIf { it.hasRecords }?.let { if (income) it.incomeMinor else it.expenseMinor }
        ?.takeIf { Regex("-?[0-9]+").matches(it) }
}

internal fun workbenchTrendMagnitude(values: List<String?>): String = values.filterNotNull()
    .mapNotNull { unsignedDigits(it.removePrefix("-")) }
    .maxWithOrNull(Comparator(::compareDigits)) ?: "0"

@Composable
private fun AnnualTrend(report: AnnualReport, showIncome: Boolean, showExpense: Boolean) {
    val income = workbenchTrendValues(report, true)
    val expense = workbenchTrendValues(report, false)
    val visible = (if (showIncome) income else emptyList()) + (if (showExpense) expense else emptyList())
    val magnitude = workbenchTrendMagnitude(visible)
    val hasNegative = visible.filterNotNull().any { it.startsWith('-') && unsignedDigits(it.removePrefix("-")) != "0" }
    val expenseColor = Color(0xFF8A3B17)
    Canvas(Modifier.fillMaxWidth().height(200.dp).semantics {
        contentDescription = "${report.year}年收入与净支出趋势。横轴1至12月；无记录或未提供月份为空心标记和断线，净支出可为负。精确金额见月份按钮。"
    }) {
        val left = 12.dp.toPx()
        val right = size.width - left
        val top = 12.dp.toPx()
        val bottom = size.height - top
        val zeroY = if (hasNegative) (top + bottom) / 2f else bottom
        val scale = if (hasNegative) (bottom - top) / 2f else bottom - top
        drawLine(Ink.copy(alpha = 0.5f), Offset(left, zeroY), Offset(right, zeroY), 1.dp.toPx())
        fun drawSeries(values: List<String?>, color: Color) {
            var previous: Offset? = null
            values.forEachIndexed { index, amount ->
                val x = left + (right - left) * index / 11f
                if (amount == null) {
                    drawCircle(color, 4.dp.toPx(), Offset(x, zeroY), style = Stroke(1.5.dp.toPx()))
                    previous = null
                } else {
                    val ratio = grossSharePermille(amount.removePrefix("-"), magnitude) / 1000f
                    val y = zeroY - ratio * scale * if (amount.startsWith('-')) -1f else 1f
                    val point = Offset(x, y)
                    previous?.let { drawLine(color, it, point, 2.dp.toPx()) }
                    drawCircle(color, 3.dp.toPx(), point)
                    previous = point
                }
            }
        }
        if (showIncome) drawSeries(income, Pine)
        if (showExpense) drawSeries(expense, expenseColor)
    }
}

@Composable
private fun AnnualTrendSection(report: AnnualReport, showIncome: Boolean, showExpense: Boolean) {
    val visible = (if (showIncome) workbenchTrendValues(report, true) else emptyList()) +
        (if (showExpense) workbenchTrendValues(report, false) else emptyList())
    val magnitude = workbenchTrendMagnitude(visible)
    val negative = visible.filterNotNull().any { it.startsWith('-') && unsignedDigits(it.removePrefix("-")) != "0" }
    Text("有记录月份 ${workbenchYearSlots(report).count { it?.hasRecords == true }}/12", color = Ink)
    Text("纵轴：${if (negative) workbenchMoney("-$magnitude") else workbenchMoney("0")} 至 ${workbenchMoney(magnitude)} · 基线0", color = Ink)
    AnnualTrend(report, showIncome, showExpense)
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        (1..12).forEach { month -> Text("$month", color = Ink, style = MaterialTheme.typography.caption) }
    }
    Text("横轴：1–12月 · 实心：有记录 · 空心与断线：无记录或数据未提供", color = Ink)
    if (visible.filterNotNull().isEmpty()) Text("暂无可绘制记录；未将缺月视为零。", color = Ink)
    else if (magnitude == "0") Text("已显示系列金额均为零。", color = Ink)
    if (!showIncome && !showExpense) Text("已隐藏全部曲线；下方月份仍可选择。", color = Ink)
}

@Composable
private fun SelectedMonthCategories(report: AnnualReport, selectedMonth: String) {
    Text("${workbenchMonthLabel(selectedMonth)}分类", color = Ink, style = MaterialTheme.typography.subtitle1)
    val month = workbenchYearSlots(report).filterNotNull().firstOrNull { it.month == selectedMonth }
    if (month == null) Text("所选月份数据未提供。", color = Ink)
    else {
        Text(workbenchMonthStatus(month), color = Ink)
        GrossCategoryPanel(month.categories, month.grossExpenseMinor)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun AnnualStatisticsPanel(report: AnnualReport, selectedMonth: String, preferences:StatisticsPreferences, onMonth: (String) -> Unit) {
    var showIncome by preferences::showIncome
    var showExpense by preferences::showExpense
    var expanded by preferences::expanded
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("${report.year}年统计", color = Ink, style = MaterialTheme.typography.h6)
        Text("收入 ${workbenchMoney(report.incomeMinor)}", color = Ink)
        Text("消费毛额 ${workbenchMoney(report.grossExpenseMinor)} · 退款 ${workbenchMoney(report.refundMinor)}", color = Ink)
        Text("净支出 ${workbenchMoney(report.expenseMinor)}", color = Ink)
        Text("结余 ${workbenchMoney(report.surplusMinor)}（非账户余额） · ${report.totalCount}笔", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WorkbenchChoice("收入曲线", showIncome) { showIncome = !showIncome }
            WorkbenchChoice("净支出曲线", showExpense) { showExpense = !showExpense }
        }
        BoxWithConstraints(Modifier.fillMaxWidth()) {
            if (maxWidth >= 760.dp) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) { AnnualTrendSection(report, showIncome, showExpense) }
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) { SelectedMonthCategories(report, selectedMonth) }
                }
            } else {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    AnnualTrendSection(report, showIncome, showExpense)
                    SelectedMonthCategories(report, selectedMonth)
                }
            }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            workbenchYearSlots(report).forEachIndexed { index, summary ->
                val key = "${report.year}-${(index + 1).toString().padStart(2, '0')}"
                val label = "${index + 1}月 · ${workbenchMonthStatus(summary)}\n收入 ${summary?.let { workbenchMoney(it.incomeMinor) } ?: "—"}\n净支出 ${summary?.let { workbenchMoney(it.expenseMinor) } ?: "—"}"
                WorkbenchChoice(label, key == selectedMonth, validWorkbenchMonth(key)) { onMonth(key) }
            }
        }
        WorkbenchChoice(if (expanded) "收起年度明细" else "展开年度明细", expanded) { expanded = !expanded }
        if (expanded) {
            workbenchYearSlots(report).forEachIndexed { index, summary ->
                Text("${index + 1}月 · ${workbenchMonthStatus(summary)}", color = Ink)
                if (summary != null) {
                    Text("收入 ${workbenchMoney(summary.incomeMinor)} · 消费毛额 ${workbenchMoney(summary.grossExpenseMinor)} · 退款 ${workbenchMoney(summary.refundMinor)}", color = Ink)
                    Text("净支出 ${workbenchMoney(summary.expenseMinor)} · 结余 ${workbenchMoney(summary.surplusMinor)}", color = Ink)
                }
                Divider()
            }
            GrossCategoryPanel(report.categories, report.grossExpenseMinor)
        }
    }
}

internal fun workbenchGrossCategoryLabel(category: CategoryTotal, grossTotal: String): String =
    "${category.name} · ${workbenchMoney(category.grossExpenseMinor)} · ${grossShareLabel(category.grossExpenseMinor, grossTotal)}"

@Composable
internal fun GrossCategoryPanel(categories: List<CategoryTotal>, grossTotal: String, onCategory: ((String) -> Unit)? = null) {
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("分类消费毛额", color = Ink, style = MaterialTheme.typography.subtitle1)
        Text("消费毛额合计 ${workbenchMoney(grossTotal)} · 占比按消费毛额计算，非净支出占比", color = Ink)
        if (categories.isEmpty()) Text("暂无分类记录", color = Ink)
        // Preserve Go ordering. amountMinor is the API net amount; never derive it here.
        categories.forEach { category ->
            val title = workbenchGrossCategoryLabel(category, grossTotal)
            if (onCategory == null) Text(title, color = Ink)
            else WorkbenchChoice(title, false) { onCategory(category.name) }
            LinearProgressIndicator(progress = grossSharePermille(category.grossExpenseMinor, grossTotal) / 1000f, modifier = Modifier.fillMaxWidth(), color = Pine)
            Text("退款 ${workbenchMoney(category.refundMinor)} · 净支出 ${workbenchMoney(category.amountMinor)}", color = Ink)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun DashboardFilterPanel(
    options: DashboardFilterOptions, filters: DashboardFilters, enabled: Boolean,
    onChange: (DashboardFilters) -> Unit, onClear: () -> Unit,
) {
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("筛选明细", color = Ink, style = MaterialTheme.typography.subtitle1)
        Text("类型", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            workbenchFilterTypes.forEach { (type, label) ->
                WorkbenchChoice(label, filters.type == type, enabled) { onChange(filters.copy(type = type)) }
            }
        }
        if (filters.type == "transfer") Text("暂无转账记录时，筛选结果为0条。", color = Ink)
        Text("账户", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WorkbenchChoice("全部账户", filters.accountId.isEmpty(), enabled) { onChange(filters.copy(accountId = "")) }
            options.accounts.forEach { account ->
                WorkbenchChoice(account.name, filters.accountId == account.id, enabled) { onChange(filters.copy(accountId = account.id)) }
            }
        }
        if (filters.accountId.isNotEmpty() && options.accounts.none { it.id == filters.accountId }) Text("所选账户已不在当前可选列表，仍保留筛选；可选择全部账户清除。", color = Ink)
        Text("分类", color = Ink)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WorkbenchChoice("全部分类", filters.category.isEmpty(), enabled) { onChange(filters.copy(category = "")) }
            options.categories.forEach { category ->
                WorkbenchChoice(category, filters.category == category, enabled) { onChange(filters.copy(category = category)) }
            }
        }
        if (filters.category.isNotEmpty() && filters.category !in options.categories) Text("所选分类：${filters.category}（当前可选列表未包含，筛选仍保留）", color = Ink)
        WorkbenchChoice("清除筛选", false, enabled && filters.active) { onClear() }
    }
}
