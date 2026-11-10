package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlin.random.Random

// These stamps describe UI intent only. Go remains the accounting authority.
internal data class ExpensePairUiCandidate(val id:String,val version:Long)
internal data class ExpensePairUiSnapshot(
    val ledgerId:String,val version:Long,val candidates:List<ExpensePairUiCandidate>,
    val page:Int,val month:String,
)

internal fun expensePairUiCandidates(dashboard:Dashboard?,ledgerId:String):List<LedgerTransaction> {
    if(dashboard==null || dashboard.ledgerId!=ledgerId)return emptyList()
    val associated=dashboard.expensePairs.filter { it.status=="active" }
        .flatMap { listOf(it.primaryId,it.secondaryId) }.toSet()
    return dashboard.transactions.filter { it.type=="expense" && it.id !in associated }
}

internal fun expensePairUiSnapshot(dashboard:Dashboard?,ledgerId:String):ExpensePairUiSnapshot? =
    dashboard?.takeIf { it.ledgerId==ledgerId }?.let { data ->
        ExpensePairUiSnapshot(ledgerId,data.version,expensePairUiCandidates(data,ledgerId)
            .map { ExpensePairUiCandidate(it.id,it.version) },data.page,data.month)
    }

internal data class ExpensePairUiPreviewTicket(
    val snapshot:ExpensePairUiSnapshot,val input:ExpensePairPreviewInput,val reason:String,
) {
    fun current(snapshot:ExpensePairUiSnapshot?,primaryId:String?,secondaryId:String?,reason:String):Boolean =
        this.snapshot==snapshot && input.primaryTransactionId==primaryId &&
            input.secondaryTransactionId==secondaryId && this.reason==reason.trim()
    fun confirmInput()=ExpensePairConfirmInput(input.ledgerVersion,input.primaryTransactionId,
        input.secondaryTransactionId,input.primaryVersion,input.secondaryVersion,reason)
}

internal fun expensePairUiPreviewTicket(snapshot:ExpensePairUiSnapshot?,primaryId:String?,secondaryId:String?,
    reason:String,readOnly:Boolean,pending:ExpensePairSubmission?):ExpensePairUiPreviewTicket? {
    if(snapshot==null || readOnly || pending!=null || reason.isBlank() || primaryId==secondaryId)return null
    val primary=snapshot.candidates.singleOrNull { it.id==primaryId } ?: return null
    val secondary=snapshot.candidates.singleOrNull { it.id==secondaryId } ?: return null
    return ExpensePairUiPreviewTicket(snapshot,ExpensePairPreviewInput(snapshot.version,
        primary.id,secondary.id,primary.version,secondary.version),reason.trim())
}

internal data class ExpensePairUiDetailTicket(val transactionId:String,val dashboardVersion:Long?,val generation:Int) {
    fun current(transactionId:String?,dashboardVersion:Long?,generation:Int):Boolean =
        this.transactionId==transactionId && this.dashboardVersion==dashboardVersion && this.generation==generation
}

internal fun expensePairUiCanWrite(readOnly:Boolean,busy:Boolean,pending:ExpensePairSubmission?,scopeMatches:Boolean=true):Boolean =
    !readOnly && !busy && pending==null && scopeMatches

internal fun expensePairUiCanWithdraw(detail:ExpensePairDetail?,dashboardVersion:Long?,receipt:ExpensePairReceipt,
    readOnly:Boolean,busy:Boolean,pending:ExpensePairSubmission?,reason:String,accepted:Boolean):Boolean =
    expensePairUiCanWrite(readOnly,busy,pending) && accepted && reason.isNotBlank() &&
        detail!=null && dashboardVersion!=null && detail.ledgerVersion>=dashboardVersion &&
        detail.decisions.any { it==receipt && it.status=="active" }

internal fun expensePairUiPendingMatches(pending:ExpensePairSubmission?,userId:String,ledgerId:String):Boolean =
    pending==null || (pending.scope.userId==userId && pending.scope.ledgerId==ledgerId)

internal fun expensePairUiRequestKey():String = List(32) { "0123456789abcdef"[Random.nextInt(16)] }.joinToString("")

internal fun expensePairUiMoney(minor:String):String = minor.toLongOrNull()?.let { formatMoney(it) } ?: minor

internal fun expensePairUiError(error:Exception):String = when((error as? LedgerException)?.code) {
    "expense_pair_not_enabled" -> "当前服务尚未启用消费关联；请保留原账单，暂不能确认或撤回。"
    "expense_pair_scope_changed" -> "账号或连接已变化。请返回原账号、原服务和账本核实上次结果；不要另建请求。"
    "expense_pair_unavailable" -> "关联资料暂不可用，已停止操作；请刷新后核实，不会改写原始凭证。"
    "forbidden" -> "当前账号没有执行此操作的权限。"
    "invalid" -> "服务拒绝了这次操作：${error.message.orEmpty()}。请刷新后重新检查选择与理由。"
    "conflict" -> "账本或关联版本已变化。请刷新并重新预览，不能沿用旧版本。"
    else -> error.message ?: "操作失败，请保留原请求后重试。"
}
