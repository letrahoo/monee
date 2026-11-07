package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

@Serializable
data class ExpensePairPreviewInput(
    val ledgerVersion: Long,
    val primaryTransactionId: String,
    val secondaryTransactionId: String,
    val primaryVersion: Long,
    val secondaryVersion: Long,
)

@Serializable
data class ExpensePairConfirmInput(
    val ledgerVersion: Long,
    val primaryTransactionId: String,
    val secondaryTransactionId: String,
    val primaryVersion: Long,
    val secondaryVersion: Long,
    val reason: String,
) {
    fun previewInput() = ExpensePairPreviewInput(ledgerVersion, primaryTransactionId,
        secondaryTransactionId, primaryVersion, secondaryVersion)
}

@Serializable
data class ExpensePairWithdrawInput(val ledgerVersion: Long, val decisionVersion: Long, val reason: String)

@Serializable
data class ExpensePairPreview(
    val ledgerVersion: Long,
    val primaryId: String,
    val secondaryId: String,
    val primaryVersion: Long,
    val secondaryVersion: Long,
    val currency: String,
    val pairGrossBeforeMinor: String,
    val pairGrossAfterMinor: String,
    val pairGrossDeltaMinor: String,
)

@Serializable
data class ExpensePairReceipt(
    val id: String,
    val version: Long,
    val status: String,
    // Version when this revision was written, not necessarily today's ledger version.
    val ledgerVersion: Long,
    val primaryId: String,
    val secondaryId: String,
)

@Serializable
data class ExpensePairDetail(val ledgerVersion: Long, val decisions: List<ExpensePairReceipt>)

// A client-side result, not a merged source record or persisted transaction.
data class ExpensePairSources(val primary: List<TransactionSource>, val secondary: List<TransactionSource>)

// Public, immutable scope permits a future UI to display exactly which account,
// origin and ledger owns an uncertain write. Never persist a credential here.
data class ExpensePairScope(val origin: String, val userId: String, val ledgerId: String)

sealed interface ExpensePairSubmission {
    val scope: ExpensePairScope
    val key: String
}

data class ExpensePairConfirmSubmission internal constructor(
    override val scope: ExpensePairScope,
    val input: ExpensePairConfirmInput,
    override val key: String,
) : ExpensePairSubmission

data class ExpensePairWithdrawSubmission internal constructor(
    override val scope: ExpensePairScope,
    val decisionId: String,
    val input: ExpensePairWithdrawInput,
    override val key: String,
) : ExpensePairSubmission

internal fun expensePairIdentifier(value: String): Boolean =
    value.isNotBlank() && value.length <= 128 && value.all { it in 'a'..'z' || it in 'A'..'Z' || it in '0'..'9' || it == '-' || it == '_' }

internal fun ExpensePairPreviewInput.valid(): Boolean = ledgerVersion >= 0 && ledgerVersion < Long.MAX_VALUE &&
    primaryVersion > 0 && secondaryVersion > 0 && primaryTransactionId != secondaryTransactionId &&
    expensePairIdentifier(primaryTransactionId) && expensePairIdentifier(secondaryTransactionId)

internal fun ExpensePairReceipt.valid(): Boolean = expensePairIdentifier(id) &&
    expensePairIdentifier(primaryId) && expensePairIdentifier(secondaryId) && primaryId != secondaryId &&
    ledgerVersion > 0 && ((status == "active" && version == 1L) || (status == "retracted" && version == 2L))

internal fun ExpensePairPreview.matches(input: ExpensePairPreviewInput): Boolean {
    val magnitude = pairGrossAfterMinor.toLongOrNull() ?: return false
    return input.valid() && ledgerVersion == input.ledgerVersion && primaryId == input.primaryTransactionId &&
        secondaryId == input.secondaryTransactionId && primaryVersion == input.primaryVersion &&
        secondaryVersion == input.secondaryVersion && currency == "CNY" && magnitude in 1L..1_000_000_000_000L &&
        pairGrossAfterMinor == magnitude.toString() && pairGrossBeforeMinor == (magnitude * 2).toString() &&
        pairGrossDeltaMinor == (-magnitude).toString()
}

internal fun ExpensePairReceipt.matches(submission: ExpensePairSubmission): Boolean = valid() && when (submission) {
    is ExpensePairConfirmSubmission -> status == "active" && version == 1L &&
        ledgerVersion == submission.input.ledgerVersion + 1 && primaryId == submission.input.primaryTransactionId &&
        secondaryId == submission.input.secondaryTransactionId
    is ExpensePairWithdrawSubmission -> status == "retracted" && id == submission.decisionId &&
        version == submission.input.decisionVersion + 1 && ledgerVersion == submission.input.ledgerVersion + 1
}

internal fun ExpensePairDetail.matches(transactionId: String): Boolean = ledgerVersion >= 0 &&
    decisions.all { it.valid() && it.ledgerVersion <= ledgerVersion && (it.primaryId == transactionId || it.secondaryId == transactionId) } &&
    decisions.map { it.id }.let { it.distinct().size == it.size && it == it.sorted() } &&
    decisions.count { it.status == "active" } <= 1
