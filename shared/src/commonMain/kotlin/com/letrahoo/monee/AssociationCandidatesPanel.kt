package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.*
import kotlinx.coroutines.CancellationException

/** A read-only inline query. Its lifecycle never uses the financial action runner. */
@Composable
internal fun AssociationCandidatesPanel(
    api: LedgerApi,
    userId: String,
    ledgerId: String,
    transaction: LedgerTransaction,
    ledgerVersion: Long,
    enabled: Boolean,
    onAccessLost: () -> Unit,
) {
    val queryScope = AssociationQueryScope(userId, ledgerId, transaction.id, transaction.version, ledgerVersion)
    val controller = remember(api, queryScope, enabled) { AssociationHintsController(queryScope) }
    var state by remember(controller) { mutableStateOf(controller.state) }
    var expanded by remember(userId, ledgerId, transaction.id) { mutableStateOf(false) }
    val accessLost by rememberUpdatedState(onAccessLost)

    fun clear() {
        controller.clear()
        state = controller.state
    }
    fun query(page: Int = 1) {
        if (enabled) {
            controller.begin(page)
            state = controller.state
        }
    }

    DisposableEffect(controller) {
        onDispose { controller.clear() }
    }
    val request = state.request
    LaunchedEffect(controller, request, expanded, enabled) {
        if (request == null || !expanded || !enabled) return@LaunchedEffect
        try {
            val response = api.associationCandidates(userId, ledgerId, transaction.id, request.page)
            if (controller.complete(request, response)) state = controller.state
        } catch (e: CancellationException) {
            controller.cancel(request)
            state = controller.state
            throw e
        } catch (e: Exception) {
            val failure = e as? LedgerException ?: LedgerException("相似项查询失败，请重试")
            if (controller.fail(request, failure)) {
                state = controller.state
                if (failure.accessLost || failure.code == "ledger_denied") accessLost()
            }
        } finally {
            if (controller.state.request == request) {
                controller.cancel(request)
                state = controller.state
            }
        }
    }

    if (!expanded) {
        TextButton(onClick = { expanded = true; query() }, enabled = enabled) {
            Text("查看相似支出")
        }
        return
    }

    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("相似支出", color = Pine)
        Text("字段相似不代表同一笔交易；不会改变账务，疑似重复仍可能计两次。", fontSize = 12.sp, color = Muted)
        Text("来源仅为展示名，角色未经验证。", fontSize = 12.sp, color = Muted)
        // Polling alone preserves this snapshot. A write, logout or disconnection
        // changes the controller key, so no old page survives even one composition.
        val data = state.data.takeIf { enabled && !state.stale }
        when {
            !enabled -> Text("当前数据暂不可查询，请稍后重新查询。", fontSize = 12.sp, color = Muted)
            state.loading -> LinearProgressIndicator(Modifier.fillMaxWidth())
            state.stale -> Text("账本或交易已变化，请等待账本刷新后重新查询。", fontSize = 12.sp, color = Muted)
            state.error != null -> Text(
                when (state.errorCode) {
                    "invalid" -> "这笔支出暂不支持相似项查询，请刷新账本后重试。"
                    "not_found" -> "该交易或查询接口不可用，请刷新账本后重试。"
                    "association_invalid" -> "相似项响应不可用，请重试。"
                    "association_stale", "association_scope_changed" -> "账本或查询范围已变化，请刷新后重新查询。"
                    "unauthenticated", "access_denied", "admin_required", "ledger_denied" -> "访问权限已变化，请返回账本重新确认。"
                    else -> "相似项查询失败，请重试。"
                }, fontSize = 12.sp, color = MaterialTheme.colors.error,
            )
            data != null -> {
                if (data.items.isEmpty()) Text("当前页没有符合条件的相似支出。", fontSize = 12.sp, color = Muted)
                data.items.forEach { candidate ->
                    val item = candidate.transaction
                    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                            Text(item.date, fontSize = 12.sp, color = Muted)
                            Spacer(Modifier.weight(1f))
                            Text(item.amountMinor.toLongOrNull()?.let { formatMoney(it) } ?: "金额不可用")
                        }
                        Text(item.merchant)
                        Text("来源展示名：${item.sourceDisplay}", fontSize = 12.sp, color = Muted)
                    }
                    Divider(color = Line)
                }
                Text("第 ${data.page} 页 · 共 ${data.totalCount} 条相似项", fontSize = 12.sp, color = Muted)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = { query(data.page - 1) }, enabled = enabled && data.page > 1) { Text("上一页") }
                    OutlinedButton(
                        onClick = { query(data.page + 1) },
                        enabled = enabled && data.page < 1_000_000 && data.page.toLong() * 50L < data.totalCount.toLong(),
                    ) { Text("下一页") }
                }
            }
            else -> Text("请重新查询当前交易的相似支出。", fontSize = 12.sp, color = Muted)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = { query() }, enabled = enabled && !state.loading) {
                Text(if (state.error != null) "重试" else if (data == null) "重新查询" else "刷新")
            }
            TextButton(onClick = { clear(); expanded = false }) { Text("收起相似支出") }
        }
    }
}
