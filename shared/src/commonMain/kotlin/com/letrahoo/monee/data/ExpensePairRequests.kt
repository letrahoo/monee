package com.letrahoo.monee.data

// One uncertain operation per scope. A changed form or switched panel must not
// replace an operation whose server result is unknown, including confirm->withdraw.
internal class ExpensePairRequests {
    private val pending = mutableMapOf<ExpensePairScope, ExpensePairSubmission>()
    fun get(scope: ExpensePairScope): ExpensePairSubmission? = pending[scope]

    fun prepareConfirm(scope: ExpensePairScope, input: ExpensePairConfirmInput, newKey: () -> String): ExpensePairConfirmSubmission {
        val previous = pending[scope]
        if (previous != null) return previous as? ExpensePairConfirmSubmission ?: throw pendingProblem()
        require(input.previewInput().valid() && input.reason.isNotBlank()) { "消费关联请求无效" }
        return ExpensePairConfirmSubmission(scope, input, checkedKey(newKey())).also { pending[scope] = it }
    }

    fun prepareWithdraw(scope: ExpensePairScope, decisionId: String, input: ExpensePairWithdrawInput, newKey: () -> String): ExpensePairWithdrawSubmission {
        val previous = pending[scope]
        if (previous != null) return previous as? ExpensePairWithdrawSubmission ?: throw pendingProblem()
        require(expensePairIdentifier(decisionId) && input.ledgerVersion >= 0 && input.ledgerVersion < Long.MAX_VALUE &&
            input.decisionVersion == 1L && input.reason.isNotBlank()) { "撤回关联请求无效" }
        return ExpensePairWithdrawSubmission(scope, decisionId, input, checkedKey(newKey())).also { pending[scope] = it }
    }

    fun clear(scope: ExpensePairScope, submitted: ExpensePairSubmission) {
        if (pending[scope] === submitted) pending.remove(scope)
    }

    private fun checkedKey(key: String): String {
        require(key.length in 16..128 && key.none { it.code < 32 || it.code == 127 }) { "缺少有效的幂等请求标识" }
        return key
    }
    private fun pendingProblem() = LedgerException("请先确认上一项消费关联操作的结果", "expense_pair_pending")
}
