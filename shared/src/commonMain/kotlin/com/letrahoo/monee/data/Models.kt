package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

@Serializable
data class LedgerTransaction(
    val id: String, val date: String, val type: String, val amountMinor: String,
    val currency: String, val merchant: String, val category: String, val source: String,
    val account: String, val externalId: String, val note: String, val version: Long,
    val refundOf: String = "",
)

@Serializable
data class TransactionInput(
    val date: String = "", val type: String = "expense", val amount: String = "",
    val currency: String = "CNY", val merchant: String = "", val category: String = "",
    val source: String = "手动记账", val account: String = "", val externalId: String = "", val note: String = "",
)

@Serializable data class CategoryTotal(val name: String, val amountMinor: String, val grossExpenseMinor:String=amountMinor, val refundMinor:String="0")
@Serializable
data class Dashboard(
    val ledgerId: String, val version: Long, val month: String, val today: String,
    val months: List<String>, val incomeMinor: String, val expenseMinor: String,
    val sourceCount: Int, val totalCount: Int, val filteredCount: Int,
    val page: Int, val pageSize: Int, val categories: List<CategoryTotal>,
    val transactions: List<LedgerTransaction>,
    val grossExpenseMinor:String=expenseMinor, val refundMinor:String="0",
)

@Serializable data class RefundInput(val version:Long,val date:String,val amount:String,val currency:String="CNY",val account:String="",val note:String="")
@Serializable data class RefundSummary(val original:LedgerTransaction,val refundedMinor:String,val remainingMinor:String,val refunds:List<LedgerTransaction>)
data class RefundSubmission(val originalId:String,val input:RefundInput,val key:String)

@Serializable data class ImportRow(val line: Int, val record: LedgerTransaction, val status: String, val message: String)
@Serializable data class PendingImportRow(val line:Int,val date:String,val merchant:String,val amount:String,val reason:String)
@Serializable data class AlipayRequest(val filename:String,val contentBase64:String,val account:String)
@Serializable data class PickedAlipay(val name:String,val contentBase64:String)
@Serializable
data class ImportPreview(
    val id: String, val filename: String, val ledgerVersion: Long, val rows: List<ImportRow>,
    val newCount: Int, val duplicateCount: Int, val similarCount: Int,
    val errors: List<String>, val alreadyCommitted: Boolean, val undone:Boolean=false,
    val pending: List<PendingImportRow> = emptyList(),
    val sourceAccount:String="",
    val format:String="standard",
    val sourceDocument:SourceDocument?=null,
)
@Serializable data class SourceDocument(val parser:String,val parserVersion:Int,val records:List<SourceRow> = emptyList())
@Serializable data class SourceRow(val line:Int,val raw:Map<String,String> = emptyMap())
@Serializable data class ImportRequest(val filename: String, val csv: String)
@Serializable data class CommitRequest(val ledgerVersion: Long, val confirmSimilar: Boolean)
@Serializable data class CommitResult(val importId: String, val added: Int, val skipped: Int, val month: String, val pending:Int = 0)
@Serializable data class Connection(val baseUrl: String)
@Serializable data class PickedCSV(val name: String, val text: String)
@Serializable data class APIProblem(val code: String = "error", val message: String = "请求失败")

@Serializable data class LoginProvider(val id:String,val enabled:Boolean)
@Serializable data class AuthUser(val id:String="",val provider:String,val subject:String,val username:String="",val email:String="",val displayName:String="",val allowed:Boolean,val role:String="") {
    val label:String get() = username.ifBlank { email.ifBlank { displayName.ifBlank { subject } } }
}
@Serializable data class AuthState(val user:AuthUser?=null,val providers:List<LoginProvider> = emptyList(),val csrfToken:String="")
@Serializable data class LoginStart(val provider:String,val client:String,val challenge:String="",val intent:String="login")
@Serializable data class LoginFlow(val id:String,val url:String,val expiresIn:Int)
@Serializable data class LoginPoll(val id:String,val verifier:String)
@Serializable data class LoginResult(val status:String,val token:String="")
data class LoginProof(val verifier:String,val challenge:String)
@Serializable data class AccessSelector(val provider:String="github",val kind:String="username",val value:String="",val note:String="")
@Serializable data class AccessEntry(val id:String,val provider:String,val kind:String,val value:String,val subject:String,val note:String,val role:String,val enabled:Boolean,val protected:Boolean,val version:Long,val createdAt:String)
@Serializable data class AccessList(val entries:List<AccessEntry>)
@Serializable data class AccessChange(val enabled:Boolean,val version:Long)
@Serializable data class AccessAudit(val id:Long,val actor:String,val action:String,val entryId:String,val createdAt:String)
@Serializable data class AccessHistory(val events:List<AccessAudit>)

@Serializable data class LedgerInfo(val id:String,val name:String,val role:String,val version:Long)
@Serializable data class LedgerList(val ledgers:List<LedgerInfo>)
@Serializable data class NameInput(val name:String)
@Serializable data class LedgerMember(val userId:String,val name:String,val role:String,val version:Long)
@Serializable data class MemberList(val members:List<LedgerMember>)
@Serializable data class MemberChange(val role:String,val version:Long)
@Serializable data class InviteInput(val userId:String,val role:String)
@Serializable data class LedgerInvitation(val id:String,val ledgerId:String,val ledgerName:String,val role:String,val invitedBy:String)
@Serializable data class InvitationList(val invitations:List<LedgerInvitation>)
@Serializable data class InvitationReply(val accept:Boolean)
@Serializable data class LinkedIdentity(val provider:String,val subject:String,val username:String="",val email:String="")
@Serializable data class PendingMerge(val id:String,val provider:String,val name:String="",val username:String="")
@Serializable data class AccountState(val user:AuthUser,val identities:List<LinkedIdentity>,val pendingMerges:List<PendingMerge>)
@Serializable data class IdentityInput(val provider:String,val subject:String)
@Serializable data class MergeInput(val id:String)

@Serializable data class RegisteredUser(val id:String,val name:String,val enabled:Boolean,val role:String,val version:Long)
@Serializable data class RegisteredUsers(val users:List<RegisteredUser>)

@Serializable data class ImportSummary(val id:String,val filename:String,val createdAt:String,val committedAt:String?=null,val result:CommitResult?=null,val errors:Int=0,val pending:Int=0,val undone:Boolean=false)
@Serializable data class ImportHistory(val imports:List<ImportSummary>,val page:Int,val pageSize:Int,val totalCount:Int)
@Serializable data class ImportDetail(val parserVersion:Int,val preview:ImportPreview)

@Serializable data class AnnotationInput(val version:Long,val category:String,val note:String)
@Serializable data class AnnotationRevision(val before:LedgerTransaction,val after:LedgerTransaction,val createdAt:String)

@Serializable data class TransactionSource(val importId:String,val filename:String,val line:Int,val disposition:String)

@Serializable data class CorrectionInput(val version:Long,val type:String,val amountMinor:String,val reason:String)
@Serializable data class CorrectionPreview(val before:LedgerTransaction,val after:LedgerTransaction,val incomeDeltaMinor:String,val expenseDeltaMinor:String,val netDeltaMinor:String)
@Serializable data class CorrectionRevision(val before:LedgerTransaction,val after:LedgerTransaction,val reason:String,val createdAt:String)

@Serializable data class ImportUndoRequest(val ledgerVersion:Long)
@Serializable data class ImportUndoRow(val transactionId:String,val date:String,val merchant:String,val type:String,val amountMinor:String,val action:String,val reason:String)
@Serializable data class ImportUndoPreview(val importId:String,val ledgerVersion:Long,val state:String,val action:String,val changeCount:Int,val preservedCount:Int,val incomeMinor:String,val expenseMinor:String,val rows:List<ImportUndoRow>,val alreadyApplied:Boolean,val blockedCount:Int=0)

@Serializable data class RedactionPreview(
    val importId:String, val localOnly:Boolean, val includedCount:Int,
    val excluded:List<RedactionExclusion>, val payload:RedactedPayload,
    val treatments:List<RedactionTreatment>,
)
@Serializable data class RedactionExclusion(val line:Int,val reason:String)
@Serializable data class RedactedPayload(val version:String,val records:List<RedactedRecord>)
@Serializable data class RedactedRecord(
    val ref:String,val date:String,val amountMinor:String,val currency:String,
    val direction:String,val source:String,val accountRef:String="",val merchantRef:String="",
    val identifiers:List<RedactedIdentifier>,
)
@Serializable data class RedactedIdentifier(val kind:String,val ref:String)
@Serializable data class RedactionTreatment(val recordRef:String,val merchant:String,val note:String,val account:String,val identifierCount:Int)

@Serializable data class ReviewQueueItem(val id:String,val importId:String,val filename:String,val line:Int,val format:String,val batchState:String,val sourceAccount:String="",val date:String,val merchant:String,val amount:String,val reason:String,val raw:Map<String,String> = emptyMap())
@Serializable data class ReviewQueue(val items:List<ReviewQueueItem>,val page:Int,val pageSize:Int,val totalCount:Int,val format:String)
