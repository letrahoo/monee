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

class LedgerApi {
    private val client = platformClient()
    private var connection: Connection? = null
    private var accessToken: String? = null
    private val sessionMutex=Mutex()
    private var sessionOrigin:String?=null
    private var csrf: String = ""

    fun reconnect() { connection = null }
    fun close() { client.close() }

    private suspend fun destination():Connection = sessionMutex.withLock {
        val next=connection ?: discoverConnection(client).also { connection=it }
        if(sessionOrigin!=next.baseUrl) {
            accessToken=loadSession(next.baseUrl);csrf="";sessionOrigin=next.baseUrl
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
        val state=apiJson.decodeFromString<AuthState>(request("auth/me"))
        csrf=state.csrfToken
        sessionMutex.withLock {
            if(state.user==null&&accessToken==tokenAtStart) {
                accessToken=null
                if(tokenAtStart!=null)clearSession(origin)
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
                        sessionMutex.withLock { accessToken=result.token;saveSession(base,result.token) }
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
        val base=destination().baseUrl
        request("auth/logout",HttpMethod.Post,"{}")
        sessionMutex.withLock { accessToken=null;csrf="";clearSession(base) }
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
    suspend fun transactionSources(ledgerId:String,id:String):List<TransactionSource> = apiJson.decodeFromString(request("transactions/$id/sources",ledgerId=ledgerId))
    suspend fun annotationHistory(ledgerId:String,id:String):List<AnnotationRevision> = apiJson.decodeFromString(request("transactions/$id/annotations",ledgerId=ledgerId))
    suspend fun importHistory(ledgerId:String,page:Int):ImportHistory = apiJson.decodeFromString(request("imports",parameters=mapOf("page" to page.toString()),ledgerId=ledgerId))
    suspend fun importDetail(ledgerId:String,id:String):ImportDetail = apiJson.decodeFromString(request("imports/$id",ledgerId=ledgerId))
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
