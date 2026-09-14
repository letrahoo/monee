package com.letrahoo.monee

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.resources.Res
import com.letrahoo.monee.resources.logo
import com.letrahoo.monee.resources.noto_sans_sc
import org.jetbrains.compose.resources.Font
import org.jetbrains.compose.resources.painterResource

private val Pine = Color(0xFF165B4A)
private val Ink = Color(0xFF20372F)
private val Muted = Color(0xFF6E7F76)
private val Paper = Color(0xFFF6F7F2)
private val Line = Color(0xFFE4E9E1)

@Composable
fun App() {
    var selectedMonth by remember { mutableStateOf(previewMonths.first()) }
    var query by remember { mutableStateOf("") }
    var selectedTransaction by remember { mutableStateOf<PreviewTransaction?>(null) }
    var showAbout by remember { mutableStateOf(false) }
    val visible = filterTransactions(selectedMonth.key, query)

    val previewFont = FontFamily(Font(Res.font.noto_sans_sc))
    MaterialTheme(
        colors = lightColors(primary = Pine, secondary = Color(0xFFE4B940), background = Paper, surface = Color.White, onBackground = Ink, onSurface = Ink),
        typography = Typography(defaultFontFamily = previewFont),
    ) {
        BoxWithConstraints(Modifier.fillMaxSize().background(Paper)) {
            val compact = maxWidth < 760.dp
            Column(
                Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(if (compact) 20.dp else 40.dp),
                verticalArrangement = Arrangement.spacedBy(26.dp),
            ) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Image(painterResource(Res.drawable.logo), "Monee Logo", Modifier.size(56.dp))
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text("Monee", fontSize = 26.sp, fontWeight = FontWeight.Bold, color = Pine)
                        Text("Know Your Money. Own Your Future.", fontSize = if (compact) 10.sp else 12.sp, color = Muted)
                    }
                    TextButton(onClick = { showAbout = !showAbout }) { Text(if (showAbout) "收起说明" else "关于预览") }
                }

                if (showAbout) {
                    Surface(color = Color.White, shape = RoundedCornerShape(12.dp)) {
                        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                            Text("Monee · 多端体验预览", fontWeight = FontWeight.Bold, color = Pine)
                            Text("可以切换月份、搜索账单、展开详情和缩放窗口。当前所有金额均为演示数据；导入、邮件接收、数据库和同步将在后续接入。", fontSize = 14.sp)
                        }
                    }
                }

                Surface(color = Color(0xFFEAF0E6), shape = RoundedCornerShape(12.dp)) {
                    Text("演示数据 · 尚未连接账本，不会读取或保存个人财务数据", Modifier.padding(horizontal = 16.dp, vertical = 12.dp), fontSize = 12.sp, color = Pine)
                }

                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("把钱看明白，把生活过从容。", fontSize = if (compact) 24.sp else 34.sp, fontWeight = FontWeight.Bold, color = Ink)
                    Text("少一点日常整理，多一点清晰判断。", fontSize = 15.sp, color = Muted)
                }

                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    previewMonths.forEach { month ->
                        if (month == selectedMonth) {
                            Button(onClick = { selectedMonth = month }, shape = RoundedCornerShape(20.dp), elevation = null) { Text(month.label) }
                        } else {
                            OutlinedButton(onClick = { selectedMonth = month }, shape = RoundedCornerShape(20.dp)) { Text(month.label) }
                        }
                    }
                }

                if (compact) {
                    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Metric("本月净支出", formatMoney(selectedMonth.expenseMinor), "已确认的演示消费", true, Modifier.fillMaxWidth())
                        Metric("本月收入", formatMoney(selectedMonth.incomeMinor), "已确认的演示收入", false, Modifier.fillMaxWidth())
                    }
                } else {
                    Row(horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                        Metric("本月净支出", formatMoney(selectedMonth.expenseMinor), "已确认的演示消费", true, Modifier.weight(1f))
                        Metric("本月收入", formatMoney(selectedMonth.incomeMinor), "已确认的演示收入", false, Modifier.weight(1f))
                        Metric("账单来源", "${previewTransactions.filter { it.month == selectedMonth.key }.map { it.source }.distinct().size} 个", "当前月份的演示来源", false, Modifier.weight(1f))
                    }
                }

                Card(shape = RoundedCornerShape(18.dp), elevation = 0.dp, modifier = Modifier.fillMaxWidth().border(1.dp, Line, RoundedCornerShape(18.dp))) {
                    Column(Modifier.padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text("值得关注", color = Pine, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                        Text(selectedMonth.insight, fontWeight = FontWeight.Medium, fontSize = 19.sp)
                        Text("基于本页合成样本展示分析方式，尚不能推断实际消费习惯。", color = Muted, fontSize = 12.sp)
                        val expenses = previewTransactions.filter { it.month == selectedMonth.key && it.amountMinor < 0 }
                        expenses.forEach { item ->
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                Text(item.category, Modifier.width(38.dp), fontSize = 12.sp, color = Muted)
                                LinearProgressIndicator(
                                    progress = (-item.amountMinor).toFloat() / selectedMonth.expenseMinor.toFloat(),
                                    modifier = Modifier.weight(1f).height(7.dp),
                                    color = Pine, backgroundColor = Color(0xFFEDF1E9),
                                )
                                Text(formatMoney(-item.amountMinor), fontSize = 12.sp, color = Ink)
                            }
                        }
                    }
                }

                Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text("账单明细", Modifier.weight(1f), fontWeight = FontWeight.Bold, fontSize = 21.sp)
                        Text("${visible.size} 笔", color = Muted, fontSize = 13.sp)
                    }
                    OutlinedTextField(
                        value = query, onValueChange = { query = it },
                        modifier = Modifier.fillMaxWidth(), singleLine = true,
                        label = { Text("搜索商户、分类或来源") },
                        shape = RoundedCornerShape(12.dp),
                        trailingIcon = { if (query.isNotEmpty()) TextButton(onClick = { query = "" }) { Text("清除") } },
                    )
                    Card(shape = RoundedCornerShape(16.dp), elevation = 0.dp, modifier = Modifier.fillMaxWidth()) {
                        Column {
                            if (visible.isEmpty()) {
                                Text("没有找到匹配的账单，试试其他关键词。", Modifier.padding(24.dp), color = Muted)
                            }
                            visible.forEachIndexed { index, transaction ->
                                TextButton(onClick = { selectedTransaction = if (selectedTransaction == transaction) null else transaction }, modifier = Modifier.fillMaxWidth(), contentPadding = PaddingValues(18.dp)) {
                                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                        if (!compact) {
                                            Text(transaction.date, Modifier.width(68.dp), color = Muted, fontSize = 13.sp)
                                        }
                                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                                            Text(transaction.merchant, color = Ink, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                            Text("${transaction.category} · ${transaction.source}${if (compact) " · ${transaction.date}" else ""}", color = Muted, fontSize = 12.sp)
                                        }
                                        Text(formatMoney(transaction.amountMinor), color = if (transaction.amountMinor > 0) Pine else Ink, fontWeight = FontWeight.SemiBold)
                                    }
                                }
                                // Keep details in the main composition: dismissing a Wasm popup can
                                // freeze accessibility updates in Compose 1.11.1 (CMP-10623).
                                if (selectedTransaction == transaction) {
                                    TransactionDetails(transaction, onClose = { selectedTransaction = null })
                                }
                                if (index < visible.lastIndex) Divider(color = Line, modifier = Modifier.padding(horizontal = 18.dp))
                            }
                        }
                    }
                    Text("上方统计覆盖整月演示数据，搜索仅筛选明细。点击账单可查看详情。", color = Muted, fontSize = 12.sp)
                }
                Text("Monee / 每一笔，都有来处。", Modifier.align(Alignment.CenterHorizontally).padding(vertical = 8.dp), color = Muted, fontSize = 12.sp)
            }
        }

    }
}

@Composable
private fun TransactionDetails(item: PreviewTransaction, onClose: () -> Unit) {
    Column(Modifier.fillMaxWidth().background(Color(0xFFF0F4EC)).padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("账单详情 · ${item.merchant}", fontWeight = FontWeight.Bold, color = Pine)
        Text("日期：${item.month.take(4)}-${item.date}")
        Text("分类：${item.category}\n来源：${item.source}\n状态：${item.note}")
        Text("这是一笔合成演示交易，尚无原始账单附件。", color = Muted, fontSize = 12.sp)
        TextButton(onClick = onClose) { Text("收起详情") }
    }
}

@Composable
private fun Metric(label: String, value: String, detail: String, emphasized: Boolean, modifier: Modifier) {
    val foreground = if (emphasized) Color.White else Ink
    Surface(color = if (emphasized) Pine else Color.White, shape = RoundedCornerShape(18.dp), modifier = modifier) {
        Column(Modifier.padding(24.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(label, color = if (emphasized) Color(0xFFD6E9DF) else Muted, fontSize = 13.sp)
            Text(value, color = foreground, fontSize = 29.sp, fontWeight = FontWeight.Bold, maxLines = 1)
            Text(detail, color = if (emphasized) Color(0xFFD6E9DF) else Muted, fontSize = 11.sp)
        }
    }
}
