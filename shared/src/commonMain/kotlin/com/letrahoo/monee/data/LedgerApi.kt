package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.request.*
import io.ktor.client.statement.bodyAsText
import io.ktor.http.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

internal val apiJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }
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
    private val client:HttpClient,
    private val discover:suspend (HttpClient)->Connection,
    private val sessionLoad:suspend (String)->String?,
    private val sessionSave:suspend (String,String)->Unit,
    private val sessionClear:suspend (String)->Unit,
) {
    constructor():this(platformClient(),{discoverConnection(it)},{loadSession(it)},{base,token->saveSession(base,token)},{clearSession(it)})
    private val associationEpoch=MutableStateFlow(0L)
    private data class AssociationReadStamp(val origin:String,val userId:String?,val generation:Long)
    private data class RequestDestination(val origin:String,val token:String?,val csrf:String,val userId:String?,val generation:Long)
    private fun invalidateAssociationReads() { associationEpoch.update { it+1L } }
    private fun ensureAssociationStamp(stamp:AssociationReadStamp) {
        if(associationEpoch.value!=stamp.generation || sessionOrigin!=stamp.origin || currentUserId!=stamp.userId || connection?.baseUrl!=stamp.origin)
            throw LedgerException("登录或连接已变化，请刷新后重新排查","association_scope_changed")
    }
    private var connection: Connection? = null
    private var accessToken: String? = null
    private val sessionMutex=Mutex()
    private var sessionOrigin:String?=null
    private var csrf: String = ""
    private var currentUserId:String?=null
    private val refundRequests=RefundRequests()

    private fun refundScope(ledgerId:String):RefundScope? =
        sessionOrigin?.let { origin->currentUserId?.let { user->RefundScope(origin,user,ledgerId) } }
    fun pendingRefund(ledgerId:String):RefundSubmission?=refundScope(ledgerId)?.let { refundRequests.get(it) }
    suspend fun prepareRefund(ledgerId:String,originalId:String,input:RefundInput,newKey:()->String):RefundSubmission {
        destination()
        val scope=refundScope(ledgerId) ?: throw LedgerException("请先刷新登录状态","unauthenticated")
        return refundRequests.prepare(scope,originalId,input,newKey)
    }

    fun reconnect() { invalidateAssociationReads(); connection = null }
    fun close() { invalidateAssociationReads(); client.close() }

    private suspend fun destination():Connection {
        while(true) {
            val generation=associationEpoch.value
            // Discovery may perform network I/O; never hold the session mutex.
            val next=connection ?: discover(client)
            val selected=sessionMutex.withLock {
                if(associationEpoch.value!=generation)return@withLock null
                val target=connection ?: next.also { connection=it }
                if(sessionOrigin!=target.baseUrl) {
                    invalidateAssociationReads()
                    val loadingGeneration=associationEpoch.value
                    val token=sessionLoad(target.baseUrl)
                    if(associationEpoch.value!=loadingGeneration)return@withLock null
                    accessToken=token;csrf="";currentUserId=null;sessionOrigin=target.baseUrl
                }
                target
            }
            if(selected!=null)return selected
        }
    }

    private suspend fun sessionStamp():AssociationReadStamp {
        val generation=associationEpoch.value
        try {
            destination()
            return sessionMutex.withLock {
                val origin=sessionOrigin ?: throw LedgerException("请先刷新登录状态","association_scope_changed")
                if(connection?.baseUrl!=origin)throw LedgerException("连接已变化，请刷新登录状态","association_scope_changed")
                AssociationReadStamp(origin,currentUserId,associationEpoch.value)
            }
        } catch(e:CancellationException) { throw e
        } catch(e:LedgerException) { throw e
        } catch(e:Throwable) {
            if(e !is Exception && !isPlatformNetworkFailure(e))throw e
            sessionMutex.withLock {
                if(associationEpoch.value==generation) { invalidateAssociationReads();connection=null }
            }
            throw LedgerException("连接失败，请确认服务已启动后重试。")
        }
    }

    private suspend fun requestDestination(expectedStamp:AssociationReadStamp?):RequestDestination {
        if(expectedStamp!=null)return sessionMutex.withLock {
            ensureAssociationStamp(expectedStamp)
            RequestDestination(expectedStamp.origin,accessToken,csrf,currentUserId,associationEpoch.value)
        }
        while(true) {
            val target=destination()
            val credentials=sessionMutex.withLock {
                if(sessionOrigin!=target.baseUrl || connection?.baseUrl!=target.baseUrl)null
                else RequestDestination(target.baseUrl,accessToken,csrf,currentUserId,associationEpoch.value)
            }
            if(credentials!=null)return credentials
        }
    }

    private suspend fun request(path: String, method: HttpMethod = HttpMethod.Get, body: String? = null,
                                parameters: Map<String, String> = emptyMap(), key: String? = null, ledgerId:String?=null,
                                expectedStamp:AssociationReadStamp?=null): String {
        var started:RequestDestination?=null
        val generationAtStart=associationEpoch.value
        try {
            val destination=requestDestination(expectedStamp)
            started=destination
            val response = client.request(destination.origin + "/api/v1/" + path) {
                expectedStamp?.let { ensureAssociationStamp(it) }
                this.method = method
                destination.token?.let { header(HttpHeaders.Authorization, "Bearer $it") }
                if (destination.csrf.isNotEmpty()) header("X-Monee-CSRF",destination.csrf)
                if (key != null) header("Idempotency-Key",key)
                if (ledgerId != null) header("X-Monee-Ledger",ledgerId)
                url { parameters.forEach { (name,value) -> this.parameters.append(name,value) } }
                if (body != null) {contentType(ContentType.Application.Json);setBody(body)}
            }
            val text=response.bodyAsText()
            expectedStamp?.let { ensureAssociationStamp(it) }
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
            sessionMutex.withLock {
                // A late failure from A cannot disconnect a newer B session.
                if(expectedStamp!=null)ensureAssociationStamp(expectedStamp)
                val sent=started
                if((sent!=null && associationEpoch.value==sent.generation && sessionOrigin==sent.origin && currentUserId==sent.userId) || (sent==null && associationEpoch.value==generationAtStart)) {
                    invalidateAssociationReads()
                    connection=null
                }
            }
            throw LedgerException("连接失败，请确认服务已启动后重试。")
        }
    }

    suspend fun authState(): AuthState {
        val stamp=sessionStamp()
        val state=apiJson.decodeFromString<AuthState>(request("auth/me",expectedStamp=stamp))
        sessionMutex.withLock {
            ensureAssociationStamp(stamp)
            if(currentUserId!=state.user?.id)invalidateAssociationReads()
            currentUserId=state.user?.id
            csrf=state.csrfToken
            if(state.user==null) {
                val hadToken=accessToken!=null
                accessToken=null
                if(hadToken)sessionClear(stamp.origin)
            }
        }
        return state
    }
    suspend fun login(provider:String,intent:String="login") {
        invalidateAssociationReads()
        val stamp=sessionStamp()
        val proof=loginProof()
        val flow=apiJson.decodeFromString<LoginFlow>(request("auth/start",HttpMethod.Post,apiJson.encodeToString(LoginStart(provider,loginClient,proof.challenge,intent)),expectedStamp=stamp))
        val base=stamp.origin
        if (!flow.url.startsWith(base+"/auth/begin?ticket=")) throw LedgerException("无效的登录地址")
        openLoginURL(flow.url)
        if(loginClient=="desktop") withTimeout(600_000) {
            while(true) {
                delay(2000)
                val result=apiJson.decodeFromString<LoginResult>(request("auth/native/poll",HttpMethod.Post,apiJson.encodeToString(LoginPoll(flow.id,proof.verifier)),expectedStamp=stamp))
                when(result.status) {
                    "complete" -> {
                        if(result.token.isBlank())throw LedgerException("登录未完成")
                        sessionMutex.withLock {
                            ensureAssociationStamp(stamp)
                            invalidateAssociationReads()
                            accessToken=result.token;csrf="";currentUserId=null
                            sessionSave(base,result.token)
                        }
                        returnToApplication()
                        return@withTimeout
                    }
                    "linked", "merge_required" -> {
                        sessionMutex.withLock { ensureAssociationStamp(stamp);invalidateAssociationReads() }
                        returnToApplication(); return@withTimeout
                    }
                    "failed" -> throw LedgerException("登录已取消或验证失败，请重试")
                }
            }
        }
    }
    suspend fun logout() {
        // Invalidate at BEGIN, including when logout later fails or is cancelled.
        invalidateAssociationReads()
        val stamp=sessionStamp()
        request("auth/logout",HttpMethod.Post,"{}",expectedStamp=stamp)
        sessionMutex.withLock {
            ensureAssociationStamp(stamp)
            invalidateAssociationReads()
            accessToken=null;csrf="";currentUserId=null;sessionClear(stamp.origin)
        }
    }

    suspend fun associationCandidates(expectedUserId:String,ledgerId:String,id:String,page:Int):AssociationCandidates {
        // Bind the caller before discovery can suspend. A read begun for A must
        // not silently retry onto B, even if both origins report the same user ID.
        val entryGeneration=associationEpoch.value
        fun canonical(value:String)=value.isNotEmpty() && value.trim()==value && value.none { it.category==CharCategory.CONTROL }
        if(!canonical(expectedUserId) || !canonical(ledgerId) || !canonical(id) || id=="." || id==".." || id.any { it in "/?#%\\\\" } || page !in 1..1_000_000)
            throw LedgerException("相似提示请求无效","association_invalid")
        val stamp=sessionStamp()
        if(stamp.generation!=entryGeneration || stamp.userId!=expectedUserId)throw LedgerException("登录状态或连接已变化，请刷新后重新排查","association_scope_changed")
        val raw=request("transactions/${id.encodeURLPathPart()}/association-candidates",parameters=mapOf("page" to page.toString()),ledgerId=ledgerId,expectedStamp=stamp)
        val result=try { apiJson.decodeFromString<AssociationCandidates>(raw) }
            catch(e:SerializationException) { throw LedgerException("相似提示响应无效，请刷新后重试","association_invalid") }
        ensureAssociationStamp(stamp)
        if(!validAssociationEnvelope(result,id,page))throw LedgerException("相似提示响应无效，请刷新后重试","association_invalid")
        return result
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
