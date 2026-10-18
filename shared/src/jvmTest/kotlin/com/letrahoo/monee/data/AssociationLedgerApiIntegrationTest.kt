package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.request.HttpRequestData
import io.ktor.http.*
import java.io.IOException
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.*
import kotlinx.serialization.encodeToString
import kotlin.test.*

class AssociationLedgerApiIntegrationTest {
    private data class Reply(val body:String,val status:HttpStatusCode=HttpStatusCode.OK)
    private fun auth(user:String?):String = if(user==null) """{"user":null,"providers":[],"csrfToken":""}""" else
        """{"user":{"id":"$user","provider":"github","subject":"synthetic-subject","allowed":true},"providers":[],"csrfToken":"synthetic-csrf"}"""
    private fun page()=AssociationCandidates(1,true,"current_transaction_similarity",false,emptyList(),2,
        AssociationTransaction("base",1,"2026-09-14","expense","-1234","CNY","合成商户","来源A"),
        listOf(AssociationCandidate(AssociationTransaction("other",1,"2026-09-14","expense","-1234","CNY","合成商户","来源B"),listOf("current_projection_match","different_source_display"))),1,50,1)

    private inner class Fixture(withTokens:Boolean=false) {
        var origin="https://a.example.test"
        var user:String?="user-a"
        var discoverPlan:suspend ()->Connection={Connection(origin)}
        val requests=CopyOnWriteArrayList<HttpRequestData>()
        val loads=CopyOnWriteArrayList<String>()
        val clears=CopyOnWriteArrayList<String>()
        var authPlan:suspend ()->String={auth(user)}
        var hintPlan:suspend (HttpRequestData)->Reply={Reply(apiJson.encodeToString(page()))}
        val client=HttpClient(MockEngine { request->
            requests+=request
            val reply=when(request.url.encodedPath) {
                "/api/v1/auth/me"->Reply(authPlan())
                "/api/v1/auth/logout"->Reply("{}")
                "/api/v1/transactions/base/association-candidates"->hintPlan(request)
                else->error("Unexpected synthetic test request path: ${request.url.encodedPath}")
            }
            respond(reply.body,reply.status,headersOf(HttpHeaders.ContentType,"application/json"))
        })
        val api=LedgerApi(client=client,discover={discoverPlan()},sessionLoad={base->
            loads+=base
            if(withTokens) when(base){"https://a.example.test"->"synthetic-token-a";"https://b.example.test"->"synthetic-token-b";else->error("Unknown synthetic origin")} else null
        },sessionSave={_,_->error("Read-only tests must not save sessions")},sessionClear={base->clears+=base})
        fun close()=api.close()
        fun hints()=requests.filter {it.url.encodedPath.endsWith("/association-candidates")}
    }
    private fun checked(block:suspend CoroutineScope.()->Unit)=runBlocking { withTimeout(10_000,block) }
    private suspend fun rejected(action:suspend ()->Unit) {
        val error=try{action();null}catch(e:Throwable){e}
        assertTrue(error is CancellationException || error is LedgerException && error.code=="association_scope_changed","Old-scope read must be rejected, got $error")
    }

    @Test fun realApiSendsOnlyScopedGetAndInjectedSession()=checked {
        val f=Fixture(withTokens=true)
        try {
            f.api.authState()
            val result=f.api.associationCandidates("user-a","ledger-a","base",1)
            assertEquals("other",result.items.single().transaction.id)
            val request=f.hints().single()
            assertEquals(HttpMethod.Get,request.method)
            assertEquals("1",request.url.parameters["page"])
            assertEquals("ledger-a",request.headers["X-Monee-Ledger"])
            assertEquals("Bearer synthetic-token-a",request.headers[HttpHeaders.Authorization])
            assertEquals(listOf("https://a.example.test"),f.loads.toList())
            assertTrue(f.clears.isEmpty())
        } finally {f.close()}
    }
    @Test fun newReadAfterCompletedSameOriginReconnectStillWorks()=checked {
        val f=Fixture()
        try {
            f.api.authState()
            f.api.reconnect()
            assertEquals("other",f.api.associationCandidates("user-a","ledger-a","base",1).items.single().transaction.id)
            assertEquals(1,f.hints().size)
        } finally {f.close()}
    }
    @Test fun delayedHintCannotSurviveLogoutBeginning()=checked {
        val f=Fixture();val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        try {
            f.api.authState()
            f.hintPlan={seen.complete(Unit);release.await();Reply(apiJson.encodeToString(page()))}
            val old=async {runCatching {f.api.associationCandidates("user-a","ledger-a","base",1)}}
            seen.await();f.api.logout();release.complete(Unit)
            rejected {old.await().getOrThrow()}
            assertEquals(listOf("https://a.example.test"),f.clears.toList())
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun delayedHintCannotSurviveSameOriginReconnect()=checked {
        val f=Fixture();val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        try {
            f.api.authState()
            f.hintPlan={seen.complete(Unit);release.await();Reply(apiJson.encodeToString(page()))}
            val old=async {runCatching {f.api.associationCandidates("user-a","ledger-a","base",1)}}
            seen.await();f.api.reconnect();release.complete(Unit)
            rejected {old.await().getOrThrow()}
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun originAndCredentialsStayBoundAndOldResponseIsDropped()=checked {
        val f=Fixture(withTokens=true);val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        try {
            f.api.authState()
            f.hintPlan={req->if(req.url.host=="a.example.test"){seen.complete(Unit);release.await()};Reply(apiJson.encodeToString(page()))}
            val old=async {runCatching {f.api.associationCandidates("user-a","ledger-a","base",1)}}
            seen.await();f.origin="https://b.example.test";f.user="user-b";f.api.reconnect();f.api.authState()
            f.api.associationCandidates("user-b","ledger-b","base",1)
            release.complete(Unit);rejected {old.await().getOrThrow()}
            f.requests.forEach {request->when(request.url.host){
                "a.example.test"->assertEquals("Bearer synthetic-token-a",request.headers[HttpHeaders.Authorization])
                "b.example.test"->assertEquals("Bearer synthetic-token-b",request.headers[HttpHeaders.Authorization])
                else->fail("Unexpected network origin")
            }}
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun staleAuthMeCannotRestorePreviousHintIdentity()=checked {
        val f=Fixture();val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        try {
            f.api.authState()
            f.authPlan={val captured=auth("user-a");seen.complete(Unit);release.await();captured}
            val oldAuth=async {runCatching {f.api.authState()}}
            seen.await();f.api.logout();f.user="user-b";f.authPlan={auth(f.user)};f.api.authState()
            release.complete(Unit);rejected {oldAuth.await().getOrThrow()}
            val page=f.api.associationCandidates("user-b","ledger-b","base",1)
            assertEquals("base",page.base.id)
            val before=f.hints().size
            rejected {f.api.associationCandidates("user-a","ledger-a","base",1)}
            assertEquals(before,f.hints().size,"Mismatched expected user must fail before issuing a GET")
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun oldTransportFailureDoesNotResetNewOriginConnection()=checked {
        val f=Fixture();val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>();val injected=AtomicBoolean(false)
        try {
            f.api.authState()
            f.hintPlan={req->if(req.url.host=="a.example.test"){seen.complete(Unit);release.await();injected.set(true);throw IOException("synthetic old network failure")};Reply(apiJson.encodeToString(page()))}
            val old=async {runCatching {f.api.associationCandidates("user-a","ledger-a","base",1)}}
            seen.await();f.origin="https://b.example.test";f.user="user-b";f.api.reconnect();f.api.authState()
            val loads=f.loads.size;release.complete(Unit);rejected {old.await().getOrThrow()}
            f.api.associationCandidates("user-b","ledger-b","base",1)
            assertTrue(injected.get(),"This case must exercise a real delayed IOException, not only cancellation")
            assertEquals(loads,f.loads.size,"Old-origin failure must not clear the current connection")
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun permissionFailureAndCancellationNeverBecomeEmptySuccess()=checked {
        val f=Fixture()
        try {
            f.api.authState()
            f.hintPlan={Reply("""{"code":"ledger_denied","message":"synthetic denied"}""",HttpStatusCode.Forbidden)}
            val denied=try{f.api.associationCandidates("user-a","ledger-a","base",1);null}catch(e:LedgerException){e}
            assertEquals("ledger_denied",denied?.code)
            f.hintPlan={throw CancellationException("synthetic cancel")}
            assertFailsWith<CancellationException>{f.api.associationCandidates("user-a","ledger-a","base",1)}
            f.hintPlan={Reply(apiJson.encodeToString(page()))}
            assertEquals(1,f.api.associationCandidates("user-a","ledger-a","base",1).totalCount)
        } finally {f.close()}
    }
    @Test fun discoveringReadCannotRetargetAcrossReconnectWithSameUserId()=checked {
        val f=Fixture(withTokens=true);val seen=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
        try {
            f.api.authState()
            f.api.reconnect()
            f.discoverPlan={val captured=f.origin;if(captured=="https://a.example.test"){seen.complete(Unit);release.await()};Connection(captured)}
            val old=async {runCatching {f.api.associationCandidates("user-a","ledger-a","base",1)}}
            seen.await();f.origin="https://b.example.test";f.api.reconnect();f.api.authState()
            release.complete(Unit)
            rejected {old.await().getOrThrow()}
            assertTrue(f.hints().isEmpty(),"Read begun in A must not be silently redirected into B, even with the same user UUID")
        } finally {release.complete(Unit);f.close()}
    }
    @Test fun pathAndPageGuardsRejectBeforeAnyDispatch()=checked {
        val f=Fixture()
        try {
            f.api.authState()
            val before=f.requests.size
            for(id in listOf(".","..","x/y","x?month=2026-09","x%2Fy","x\\y","x#fragment","\u0000")) {
                val error=try{f.api.associationCandidates("user-a","ledger-a",id,1);null}catch(e:LedgerException){e}
                assertNotNull(error,"Unsafe path segment must fail locally")
            }
            for(number in listOf(0,-1,1_000_001)) {
                val error=try{f.api.associationCandidates("user-a","ledger-a","base",number);null}catch(e:LedgerException){e}
                assertNotNull(error)
            }
            assertEquals(before,f.requests.size,"Invalid path/page must not reach any alternate endpoint")
        } finally {f.close()}
    }
    @Test fun invalidPayloadNeverReturnsAReadableHintPage()=checked {
        val f=Fixture()
        try {
            f.api.authState()
            for(body in listOf("{}",apiJson.encodeToString(page().copy(readOnly=false)),apiJson.encodeToString(page().copy(actions=listOf("merge"))))){
                f.hintPlan={Reply(body)}
                val error=try{f.api.associationCandidates("user-a","ledger-a","base",1);null}catch(e:Throwable){e}
                assertTrue(error is LedgerException && error.code=="association_invalid","Malformed contract must fail with association_invalid, got $error")
            }
        } finally {f.close()}
    }
}
