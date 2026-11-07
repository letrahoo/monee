package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.request.*
import io.ktor.client.statement.bodyAsText
import io.ktor.http.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

internal val apiJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }
// Finish platform file access before constructing the client on the UI thread.
internal expect suspend fun preparePlatformClient()
internal expect fun platformClient(): HttpClient
internal expect suspend fun discoverConnection(client: HttpClient): Connection
expect suspend fun chooseCSV(): PickedCSV?
expect suspend fun chooseNativeBill(format:String): PickedAlipay?
internal expect val loginClient: String
internal expect fun loginProof(): LoginProof
internal expect suspend fun openLoginURL(url:String)
internal expect suspend fun returnToApplication()
internal expect fun isPlatformNetworkFailure(cause: Throwable): Boolean

internal expect suspend fun loadSession(base:String):String?
internal expect suspend fun saveSession(base:String,token:String)
internal expect suspend fun clearSession(base:String)

class LedgerApi internal constructor(
    private val client: HttpClient,
    private val discover: suspend (HttpClient) -> Connection,
    private val readSession: suspend (String) -> String?,
    private val writeSession: suspend (String, String) -> Unit,
    private val removeSession: suspend (String) -> Unit,
) {
    // Production always uses the platform transport, discovery and secure vault.
    // Internal injection lets isolated tests exercise the real request lifecycle.
    constructor() : this(platformClient(), ::discoverConnection, ::loadSession, ::saveSession, ::clearSession)
    private var connection: Connection? = null
    private var accessToken: String? = null
    private val sessionMutex=Mutex()
    private var sessionOrigin:String?=null
    private var csrf: String = ""
    private var currentUserId:String?=null
    private val refundRequests=RefundRequests()
    private val transferRequests=TransferRequests()
    private val expensePairRequests=ExpensePairRequests()
    // Changes immediately on reconnect and on identity/origin changes. Pair
    // calls capture this before their first suspension and pin credentials.
    private var expensePairEpoch=0L

    private data class ExpensePairStamp(val scope:ExpensePairScope,val epoch:Long,val token:String?,val csrf:String)
    private fun expensePairScope(ledgerId:String):ExpensePairScope? = sessionOrigin?.let { origin ->
        currentUserId?.let { user -> ExpensePairScope(origin,user,ledgerId) }
    }
    private fun expensePairScopeChanged() = LedgerException("账号或连接已变化，请返回原账本核实操作结果", "expense_pair_scope_changed")
    private fun expensePairStampCurrent(stamp:ExpensePairStamp):Boolean =
        expensePairEpoch==stamp.epoch && expensePairScope(stamp.scope.ledgerId)==stamp.scope &&
            accessToken==stamp.token && csrf==stamp.csrf
    private suspend fun expensePairStamp(ledgerId:String):ExpensePairStamp {
        require(expensePairIdentifier(ledgerId)) { "账本标识无效" }
        val epoch=expensePairEpoch
        destination()
        if(epoch!=expensePairEpoch)throw expensePairScopeChanged()
        val scope=expensePairScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        return ExpensePairStamp(scope,epoch,accessToken,csrf)
    }

    fun pendingExpensePair(ledgerId:String):ExpensePairSubmission? = expensePairScope(ledgerId)?.let { expensePairRequests.get(it) }
    suspend fun prepareExpensePairConfirm(ledgerId:String,input:ExpensePairConfirmInput,newKey:()->String):ExpensePairConfirmSubmission {
        val stamp=expensePairStamp(ledgerId)
        return expensePairRequests.prepareConfirm(stamp.scope,input,newKey)
    }
    suspend fun prepareExpensePairWithdraw(ledgerId:String,decisionId:String,input:ExpensePairWithdrawInput,newKey:()->String):ExpensePairWithdrawSubmission {
        val stamp=expensePairStamp(ledgerId)
        return expensePairRequests.prepareWithdraw(stamp.scope,decisionId,input,newKey)
    }

    suspend fun previewExpensePair(ledgerId:String,input:ExpensePairPreviewInput):ExpensePairPreview {
        require(input.valid()) { "消费关联预览请求无效" }
        val stamp=expensePairStamp(ledgerId)
        val result=apiJson.decodeFromString<ExpensePairPreview>(expensePairRequest(stamp,"expense-pairs/preview",HttpMethod.Post,apiJson.encodeToString(input)))
        if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
        if(!result.matches(input))throw LedgerException("消费关联预览响应无效，请刷新后重试","expense_pair_contract")
        return result
    }
    suspend fun expensePairs(ledgerId:String,transactionId:String):ExpensePairDetail {
        require(expensePairIdentifier(transactionId)) { "交易标识无效" }
        val stamp=expensePairStamp(ledgerId)
        val result=apiJson.decodeFromString<ExpensePairDetail>(expensePairRequest(stamp,"transactions/$transactionId/expense-pairs"))
        if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
        if(!result.matches(transactionId))throw LedgerException("消费关联详情响应无效，请刷新后重试","expense_pair_contract")
        return result
    }
    suspend fun expensePairSources(ledgerId:String,receipt:ExpensePairReceipt):ExpensePairSources {
        require(receipt.valid()) { "关联记录无效" }
        val stamp=expensePairStamp(ledgerId)
        // Revalidate the relationship in this scope before reading either side.
        // Both source reads use one pinned origin/session, never generic discovery.
        val detail=apiJson.decodeFromString<ExpensePairDetail>(expensePairRequest(stamp,"transactions/${receipt.primaryId}/expense-pairs"))
        if(!detail.matches(receipt.primaryId) || receipt !in detail.decisions)
            throw LedgerException("关联记录已变化，请刷新详情后重试","expense_pair_contract")
        val primary=apiJson.decodeFromString<List<TransactionSource>>(expensePairRequest(stamp,"transactions/${receipt.primaryId}/sources"))
        val secondary=apiJson.decodeFromString<List<TransactionSource>>(expensePairRequest(stamp,"transactions/${receipt.secondaryId}/sources"))
        if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
        return ExpensePairSources(primary,secondary)
    }

    suspend fun confirmExpensePair(ledgerId:String,submission:ExpensePairConfirmSubmission):ExpensePairReceipt =
        submitExpensePair(ledgerId,submission,"expense-pairs",apiJson.encodeToString(submission.input))
    suspend fun withdrawExpensePair(ledgerId:String,submission:ExpensePairWithdrawSubmission):ExpensePairReceipt =
        submitExpensePair(ledgerId,submission,"expense-pairs/${submission.decisionId}/withdraw",apiJson.encodeToString(submission.input))

    private suspend fun submitExpensePair(ledgerId:String,submission:ExpensePairSubmission,path:String,body:String):ExpensePairReceipt {
        val stamp=expensePairStamp(ledgerId)
        require(submission.scope==stamp.scope && expensePairRequests.get(stamp.scope)===submission) { "消费关联请求不属于当前账号或账本" }
        try {
            val result=apiJson.decodeFromString<ExpensePairReceipt>(expensePairRequest(stamp,path,HttpMethod.Post,body,submission.key))
            if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
            if(!result.matches(submission))throw LedgerException("消费关联操作结果尚未确认，请保留原请求重试","expense_pair_contract")
            expensePairRequests.clear(stamp.scope,submission)
            return result
        } catch(e:LedgerException) {
            // No uncertain network/server/contract/cancellation response can replace
            // the exact input or key. A late old-scope rejection clears nothing.
            if(expensePairStampCurrent(stamp) && e.code in listOf("invalid","conflict"))expensePairRequests.clear(stamp.scope,submission)
            throw e
        }
    }

    private suspend fun expensePairRequest(stamp:ExpensePairStamp,path:String,method:HttpMethod=HttpMethod.Get,body:String?=null,key:String?=null):String {
        if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
        try {
            // Do not call request()/destination() again: discovery must never
            // silently redirect an old confirmation into a new origin/session.
            val response=client.request(stamp.scope.origin+"/api/v1/"+path) {
                this.method=method
                stamp.token?.let { header(HttpHeaders.Authorization,"Bearer $it") }
                if(stamp.csrf.isNotEmpty())header("X-Monee-CSRF",stamp.csrf)
                header("X-Monee-Ledger",stamp.scope.ledgerId)
                key?.let { header("Idempotency-Key",it) }
                if(body!=null){contentType(ContentType.Application.Json);setBody(body)}
            }
            val text=response.bodyAsText()
            if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
            if(!response.status.isSuccess()) {
                val problem=runCatching { apiJson.decodeFromString<APIProblem>(text) }.getOrDefault(APIProblem(message="本地服务返回异常，请重试"))
                throw LedgerException(problem.message,problem.code)
            }
            return text
        } catch(e:CancellationException){throw e
        } catch(e:LedgerException){if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged();throw e
        } catch(e:Throwable){
            if(!expensePairStampCurrent(stamp))throw expensePairScopeChanged()
            if(e !is Exception && !isPlatformNetworkFailure(e))throw e
            connection=null
            throw LedgerException("连接失败，消费关联结果尚未确认，请使用原请求重试。")
        }
    }

    private fun transferScope(ledgerId:String):TransferScope? =
        sessionOrigin?.let { origin->currentUserId?.let { user->TransferScope(origin,user,ledgerId) } }
    fun pendingTransfer(ledgerId:String):TransferSubmission?=transferScope(ledgerId)?.let { transferRequests.get(it) }
    suspend fun prepareTransfer(ledgerId:String,input:TransferInput,newKey:()->String):TransferSubmission {
        destination()
        val scope=transferScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        return transferRequests.prepare(scope,input,newKey)
    }
    suspend fun createTransfer(ledgerId:String,submission:TransferSubmission):LedgerTransaction {
        destination()
        val scope=transferScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        require(transferRequests.get(scope)==submission){"转账请求不属于当前账号或账本"}
        try {
            val result=apiJson.decodeFromString<LedgerTransaction>(request("transfers",HttpMethod.Post,apiJson.encodeToString(submission.input),key=submission.key,ledgerId=ledgerId))
            transferRequests.clear(scope,submission)
            return result
        } catch(e:LedgerException) {
            if(e.code in listOf("invalid","conflict"))transferRequests.clear(scope,submission)
            throw e
        }
    }

    private fun refundScope(ledgerId:String):RefundScope? =
        sessionOrigin?.let { origin->currentUserId?.let { user->RefundScope(origin,user,ledgerId) } }
    fun pendingRefund(ledgerId:String):RefundSubmission?=refundScope(ledgerId)?.let { refundRequests.get(it) }
    suspend fun prepareRefund(ledgerId:String,originalId:String,input:RefundInput,newKey:()->String):RefundSubmission {
        destination()
        val scope=refundScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        return refundRequests.prepare(scope,originalId,input,newKey)
    }

    fun reconnect() { expensePairEpoch++; connection = null }
    fun close() { client.close() }

    private suspend fun destination():Connection = sessionMutex.withLock {
        val next=connection ?: discover(client).also { connection=it }
        if(sessionOrigin!=next.baseUrl) {
            expensePairEpoch++
            accessToken=readSession(next.baseUrl);csrf="";currentUserId=null;sessionOrigin=next.baseUrl
        }
        next
    }

    private suspend fun request(path: String, method: HttpMethod = HttpMethod.Get, body: String? = null,
                                parameters: Map<String, String> = emptyMap(), key: String? = null, ledgerId:String?=null): String {
        try {
            val destination = destination()
            val response = client.request(destination.baseUrl + "/api/v1/" + path) {
                this.method = method
                accessToken?.let { header(HttpHeaders.Authorization, "Bearer $it") }
                if (csrf.isNotEmpty()) header("X-Monee-CSRF",csrf)
                if (key != null) header("Idempotency-Key",key)
                if (ledgerId != null) header("X-Monee-Ledger",ledgerId)
                url { parameters.forEach { (name,value) -> this.parameters.append(name,value) } }
                if (body != null) {contentType(ContentType.Application.Json);setBody(body)}
            }
            val text=response.bodyAsText()
            if (!response.status.isSuccess()) {
                val problem=runCatching {apiJson.decodeFromString<APIProblem>(text)}.getOrDefault(APIProblem(message="本地服务返回异常，请重试"))
                throw LedgerException(problem.message,problem.code)
            }
            return text
        } catch (e: CancellationException) { throw e
        } catch (e: LedgerException) { throw e
        } catch (e: Throwable) {
            // Ktor Wasm wraps a rejected browser fetch in Error with a JsError cause.
            // Normalize only transport/ordinary exceptions; preserve fatal runtime errors.
            if (e !is Exception && !isPlatformNetworkFailure(e)) throw e
            connection = null
            throw LedgerException("连接失败，请确认服务已启动后重试。")
        }
    }

    suspend fun authState(): AuthState {
        val origin=destination().baseUrl
        val tokenAtStart=accessToken
        val pairEpochAtStart=expensePairEpoch
        val state=apiJson.decodeFromString<AuthState>(request("auth/me"))
        // A delayed auth response must not rebind pair submissions to a newer
        // connection or resurrect the identity that has just logged out.
        if(sessionOrigin!=origin || expensePairEpoch!=pairEpochAtStart || accessToken!=tokenAtStart)throw expensePairScopeChanged()
        if(currentUserId!=state.user?.id)expensePairEpoch++
        currentUserId=state.user?.id
        csrf=state.csrfToken
        sessionMutex.withLock {
            if(state.user==null&&accessToken==tokenAtStart) {
                accessToken=null
                if(tokenAtStart!=null)removeSession(origin)
            }
        }
        return state
    }
    suspend fun login(provider:String,intent:String="login") {
        val proof=loginProof()
        val flow=apiJson.decodeFromString<LoginFlow>(request("auth/start",HttpMethod.Post,apiJson.encodeToString(LoginStart(provider,loginClient,proof.challenge,intent))))
        val base=connection?.baseUrl ?: throw LedgerException("本地连接已失效")
        if (!flow.url.startsWith(base+"/auth/begin?ticket=")) throw LedgerException("无效的登录地址")
        openLoginURL(flow.url)
        if(loginClient=="desktop") withTimeout(600_000) {
            while(true) {
                delay(2000)
                val result=apiJson.decodeFromString<LoginResult>(request("auth/native/poll",HttpMethod.Post,apiJson.encodeToString(LoginPoll(flow.id,proof.verifier))))
                when(result.status) {
                    "complete" -> {
                        if(result.token.isBlank())throw LedgerException("登录未完成")
                        sessionMutex.withLock { expensePairEpoch++;currentUserId=null;accessToken=result.token;writeSession(base,result.token) }
                        returnToApplication()
                        return@withTimeout
                    }
                    "linked", "merge_required" -> { returnToApplication(); return@withTimeout }
                    "failed" -> throw LedgerException("登录已取消或验证失败，请重试")
                }
            }
        }
    }
    suspend fun logout() {
        expensePairEpoch++;currentUserId=null
        val base=destination().baseUrl
        request("auth/logout",HttpMethod.Post,"{}")
        sessionMutex.withLock { accessToken=null;csrf="";currentUserId=null;removeSession(base) }
    }
    suspend fun registeredUsers():RegisteredUsers = apiJson.decodeFromString(request("admin/users"))
    suspend fun setRegisteredUser(user:RegisteredUser) {request("admin/users/${user.id}",HttpMethod.Patch,apiJson.encodeToString(AccessChange(!user.enabled,user.version)))}
    suspend fun accessList():AccessList = apiJson.decodeFromString(request("admin/allowlist"))
    suspend fun addAccess(input:AccessSelector):AccessEntry = apiJson.decodeFromString(request("admin/allowlist",HttpMethod.Post,apiJson.encodeToString(input)))
    suspend fun setAccess(entry:AccessEntry):AccessEntry = apiJson.decodeFromString(request("admin/allowlist/"+entry.id,HttpMethod.Patch,apiJson.encodeToString(AccessChange(!entry.enabled,entry.version))))
    suspend fun accessHistory():AccessHistory = apiJson.decodeFromString(request("admin/audit"))

    suspend fun dashboard(ledgerId:String,month: String, query: String, page: Int): Dashboard =
        apiJson.decodeFromString(request("dashboard", parameters = mapOf("month" to month, "q" to query, "page" to page.toString()),ledgerId=ledgerId))
    suspend fun preview(ledgerId:String,filename: String, csv: String): ImportPreview =
        apiJson.decodeFromString(request("imports/preview", HttpMethod.Post, apiJson.encodeToString(ImportRequest(filename,csv)),ledgerId=ledgerId))
    suspend fun annotate(ledgerId:String,id:String,input:AnnotationInput):LedgerTransaction = apiJson.decodeFromString(request("transactions/$id/annotation",HttpMethod.Patch,apiJson.encodeToString(input),ledgerId=ledgerId))
    suspend fun previewCorrection(ledgerId:String,id:String,input:CorrectionInput):CorrectionPreview = apiJson.decodeFromString(request("transactions/$id/correction/preview",HttpMethod.Post,apiJson.encodeToString(input),ledgerId=ledgerId))
    suspend fun correct(ledgerId:String,id:String,input:CorrectionInput):LedgerTransaction = apiJson.decodeFromString(request("transactions/$id/correction",HttpMethod.Patch,apiJson.encodeToString(input),ledgerId=ledgerId))
    suspend fun correctionHistory(ledgerId:String,id:String):List<CorrectionRevision> = apiJson.decodeFromString(request("transactions/$id/corrections",ledgerId=ledgerId))
    suspend fun refunds(ledgerId:String,id:String):RefundSummary = apiJson.decodeFromString(request("transactions/$id/refunds",ledgerId=ledgerId))
    suspend fun createRefund(ledgerId:String,submission:RefundSubmission):LedgerTransaction {
        destination()
        val scope=refundScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        require(refundRequests.get(scope)==submission){"退款请求不属于当前账号或账本"}
        try {
            val result=apiJson.decodeFromString<LedgerTransaction>(request("transactions/${submission.originalId}/refunds",HttpMethod.Post,apiJson.encodeToString(submission.input),key=submission.key,ledgerId=ledgerId))
            refundRequests.clear(scope,submission)
            return result
        } catch(e:LedgerException) {
            // Transport/server errors are uncertain: retain the exact request.
            if(e.code in listOf("invalid","conflict"))refundRequests.clear(scope,submission)
            throw e
        }
    }
    suspend fun transactionSources(ledgerId:String,id:String):List<TransactionSource> = apiJson.decodeFromString(request("transactions/$id/sources",ledgerId=ledgerId))
    suspend fun annotationHistory(ledgerId:String,id:String):List<AnnotationRevision> = apiJson.decodeFromString(request("transactions/$id/annotations",ledgerId=ledgerId))
    suspend fun importUndoPreview(ledgerId:String,id:String,restore:Boolean=false):ImportUndoPreview = apiJson.decodeFromString(request("imports/$id/${if(restore)"restore"else"undo"}-preview",ledgerId=ledgerId))
    suspend fun changeImportState(ledgerId:String,preview:ImportUndoPreview):ImportUndoPreview = apiJson.decodeFromString(request("imports/${preview.importId}/${preview.action}",HttpMethod.Post,apiJson.encodeToString(ImportUndoRequest(preview.ledgerVersion)),ledgerId=ledgerId))
    suspend fun reviewQueue(ledgerId:String,page:Int=1,format:String=""):ReviewQueue = apiJson.decodeFromString(request("review-queue",parameters=mapOf("page" to page.toString(),"format" to format),ledgerId=ledgerId))
    suspend fun importHistory(ledgerId:String,page:Int):ImportHistory = apiJson.decodeFromString(request("imports",parameters=mapOf("page" to page.toString()),ledgerId=ledgerId))
    suspend fun importDetail(ledgerId:String,id:String):ImportDetail = apiJson.decodeFromString(request("imports/$id",ledgerId=ledgerId))
    suspend fun redactionPreview(ledgerId:String,id:String):RedactionPreview = apiJson.decodeFromString(request("imports/$id/redaction-preview",ledgerId=ledgerId))
    suspend fun previewNative(ledgerId:String,format:String,filename:String,content:String,account:String):ImportPreview =
        apiJson.decodeFromString(request("imports/$format/preview",HttpMethod.Post,apiJson.encodeToString(AlipayRequest(filename,content,account)),ledgerId=ledgerId))
    suspend fun commit(ledgerId:String,preview: ImportPreview, confirmSimilar: Boolean): CommitResult =
        apiJson.decodeFromString(request("imports/${preview.id}/commit", HttpMethod.Post, apiJson.encodeToString(CommitRequest(preview.ledgerVersion,confirmSimilar)),ledgerId=ledgerId))
    suspend fun create(ledgerId:String,input: TransactionInput, key: String): LedgerTransaction =
        apiJson.decodeFromString(request("transactions", HttpMethod.Post, apiJson.encodeToString(input), key = key,ledgerId=ledgerId))
    suspend fun ledgers():LedgerList = apiJson.decodeFromString(request("ledgers"))
    suspend fun newLedger(name:String):LedgerInfo = apiJson.decodeFromString(request("ledgers",HttpMethod.Post,apiJson.encodeToString(NameInput(name))))
    suspend fun members(id:String):MemberList = apiJson.decodeFromString(request("ledgers/$id/members"))
    suspend fun invite(id:String,userId:String,role:String) {request("ledgers/$id/invitations",HttpMethod.Post,apiJson.encodeToString(InviteInput(userId,role)))}
    suspend fun changeMember(id:String,member:LedgerMember,role:String) {request("ledgers/$id/members/${member.userId}",HttpMethod.Patch,apiJson.encodeToString(MemberChange(role,member.version)))}
    suspend fun invitations():InvitationList = apiJson.decodeFromString(request("invitations"))
    suspend fun respond(id:String,accept:Boolean) {request("invitations/$id/respond",HttpMethod.Post,apiJson.encodeToString(InvitationReply(accept)))}
    suspend fun account():AccountState = apiJson.decodeFromString(request("auth/account"))
    suspend fun rename(name:String) {request("auth/account",HttpMethod.Patch,apiJson.encodeToString(NameInput(name)))}
    suspend fun unlink(identity:LinkedIdentity) {request("auth/account/unlink",HttpMethod.Post,apiJson.encodeToString(IdentityInput(identity.provider,identity.subject)))}
    suspend fun merge(id:String) {request("auth/account/merge",HttpMethod.Post,apiJson.encodeToString(MergeInput(id)))}

    suspend fun template(sample: Boolean): String = request("template", parameters = mapOf("sample" to sample.toString()))
}

class LedgerException(message: String,val code:String=""): Exception(message) {
    val accessLost:Boolean get() = code in listOf("unauthenticated","access_denied","admin_required")
}
