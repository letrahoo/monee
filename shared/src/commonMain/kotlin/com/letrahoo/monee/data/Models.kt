package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

@Serializable
data class LedgerTransaction(
    val id: String, val date: String, val type: String, val amountMinor: String,
    val currency: String, val merchant: String, val category: String, val source: String,
    val account: String, val externalId: String, val note: String, val version: Long,
)

@Serializable
data class TransactionInput(
    val date: String = "", val type: String = "expense", val amount: String = "",
    val currency: String = "CNY", val merchant: String = "", val category: String = "",
    val source: String = "手动记账", val account: String = "", val externalId: String = "", val note: String = "",
)

@Serializable data class CategoryTotal(val name: String, val amountMinor: String)
@Serializable
data class Dashboard(
    val ledgerId: String, val version: Long, val month: String, val today: String,
    val months: List<String>, val incomeMinor: String, val expenseMinor: String,
    val sourceCount: Int, val totalCount: Int, val filteredCount: Int,
    val page: Int, val pageSize: Int, val categories: List<CategoryTotal>,
    val transactions: List<LedgerTransaction>,
)

@Serializable data class ImportRow(val line: Int, val record: LedgerTransaction, val status: String, val message: String)
@Serializable
data class ImportPreview(
    val id: String, val filename: String, val ledgerVersion: Long, val rows: List<ImportRow>,
    val newCount: Int, val duplicateCount: Int, val similarCount: Int,
    val errors: List<String>, val alreadyCommitted: Boolean,
)
@Serializable data class ImportRequest(val filename: String, val csv: String)
@Serializable data class CommitRequest(val ledgerVersion: Long, val confirmSimilar: Boolean)
@Serializable data class CommitResult(val importId: String, val added: Int, val skipped: Int, val month: String)
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
