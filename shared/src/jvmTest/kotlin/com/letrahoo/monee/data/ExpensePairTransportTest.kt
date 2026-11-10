package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.*
import io.ktor.http.content.TextContent
import kotlinx.coroutines.*
import kotlinx.serialization.encodeToString
import java.io.IOException
import kotlin.test.*

class ExpensePairTransportTest {
 private val input=ExpensePairConfirmInput(7,"primary-a","secondary-a",1,1,"synthetic confirmation")
 private val receipt=ExpensePairReceipt("decision-a",1,"active",8,"primary-a","secondary-a")
 private class Fixture {
  var origin="https://synthetic.invalid"
  var user="synthetic-user-a"
  var discoverPause:(suspend ()->Unit)?=null
  data class Sent(val url:String,val key:String?,val body:String,val auth:String?,val ledger:String?)
  val sent=mutableListOf<Sent>()
  var submit:suspend ()->Pair<HttpStatusCode,String> = {HttpStatusCode.Created to "{}"}
  val client=HttpClient(MockEngine { request ->
   when(request.url.encodedPath){
    "/api/v1/auth/me" -> respond(apiJson.encodeToString(AuthState(AuthUser(user,"github","synthetic-sub",allowed=true),csrfToken="synthetic-csrf")))
    "/api/v1/auth/logout" -> respond("{}")
    else -> {sent+=Sent(request.url.toString(),request.headers["Idempotency-Key"],(request.body as? TextContent)?.text.orEmpty(),request.headers[HttpHeaders.Authorization],request.headers["X-Monee-Ledger"]);val(status,body)=submit();respond(body,status)}
   }
  })
  val removed=mutableListOf<String>()
  val api=LedgerApi(client,{ discoverPause?.invoke();Connection(origin) },{"synthetic-token"},{_,_->},{removed+=it})
 }
 @Test fun unknownResultKeepsExactKeyAndInputUntilValidResponse():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState()
   val pending=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-confirm-key"}
   f.submit={throw IOException("synthetic lost result")}
   assertFailsWith<LedgerException>{f.api.confirmExpensePair("ledger-a",pending)}
   assertSame(pending,f.api.pendingExpensePair("ledger-a"))
   assertSame(pending,f.api.prepareExpensePairConfirm("ledger-a",input.copy(reason="different")){error("must not replace key")})
   f.submit={HttpStatusCode.Created to apiJson.encodeToString(receipt)}
   assertEquals(receipt,f.api.confirmExpensePair("ledger-a",pending))
   assertNull(f.api.pendingExpensePair("ledger-a"))
   assertEquals(2,f.sent.size);assertEquals(f.sent[0],f.sent[1])
   assertEquals("Bearer synthetic-token",f.sent[0].auth);assertEquals("ledger-a",f.sent[0].ledger)
  }finally{f.api.close()}
 }
 @Test fun malformedOrMismatchedReceiptDoesNotClearPending():Unit=runBlocking{
  for(body in listOf("not-json",apiJson.encodeToString(receipt.copy(primaryId="different")))){
   val f=Fixture();try{f.api.authState();val p=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-receipt-key"};f.submit={HttpStatusCode.Created to body};assertFailsWith<Exception>{f.api.confirmExpensePair("ledger-a",p)};assertSame(p,f.api.pendingExpensePair("ledger-a"))}finally{f.api.close()}
  }
 }
 @Test fun changedOriginOrUserCannotReceiveOldSubmission():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState();val p=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-scope-key"}
   f.origin="https://other-synthetic.invalid";f.api.reconnect();f.api.authState()
   assertFailsWith<IllegalArgumentException>{f.api.confirmExpensePair("ledger-a",p)};assertTrue(f.sent.isEmpty())
   f.origin="https://synthetic.invalid";f.api.reconnect();f.api.authState();f.user="synthetic-user-b";f.api.authState()
   assertFailsWith<IllegalArgumentException>{f.api.confirmExpensePair("ledger-a",p)};assertTrue(f.sent.isEmpty())
  }finally{f.api.close()}
 }
 @Test fun discoveryPauseCannotRedirectOldRead():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState();f.api.reconnect()
   val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
   f.discoverPause={started.complete(Unit);release.await()}
   val read=async{runCatching{f.api.expensePairs("ledger-a","primary-a")}}
   withTimeout(5000){started.await()};f.origin="https://new-synthetic.invalid";f.api.reconnect();release.complete(Unit)
   val error=read.await().exceptionOrNull();assertIs<LedgerException>(error);assertEquals("expense_pair_scope_changed",error.code);assertTrue(f.sent.isEmpty())
  }finally{f.api.close()}
 }
 @Test fun serverFailureAndCancellationKeepPending():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState();val p=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-cancel-key"}
   f.submit={HttpStatusCode.InternalServerError to """{"code":"internal","message":"synthetic"}"""}
   assertFailsWith<LedgerException>{f.api.confirmExpensePair("ledger-a",p)};assertSame(p,f.api.pendingExpensePair("ledger-a"))
   val started=CompletableDeferred<Unit>();f.submit={started.complete(Unit);awaitCancellation()}
   val job=launch{f.api.confirmExpensePair("ledger-a",p)}
   withTimeout(5000){started.await()};job.cancelAndJoin();assertSame(p,f.api.pendingExpensePair("ledger-a"))
  }finally{f.api.close()}
 }
 @Test fun lateSuccessKeepsOldPendingForReconciliation():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState();val p=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-late-success-key"}
   val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
   f.submit={started.complete(Unit);release.await();HttpStatusCode.Created to apiJson.encodeToString(receipt)}
   val job=async{runCatching{f.api.confirmExpensePair("ledger-a",p)}}
   withTimeout(5000){started.await()};f.user="synthetic-user-b";f.api.authState();release.complete(Unit)
   val error=job.await().exceptionOrNull();assertIs<LedgerException>(error);assertEquals("expense_pair_scope_changed",error.code)
   f.user="synthetic-user-a";f.api.authState();assertSame(p,f.api.pendingExpensePair("ledger-a"))
  }finally{f.api.close()}
 }
 @Test fun lateUnauthorizedFromOldScopeCannotClearNewIdentity():Unit=runBlocking{
  val f=Fixture();try{
   f.api.authState();val pending=f.api.prepareExpensePairConfirm("ledger-a",input){"synthetic-late-key"}
   val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
   f.submit={started.complete(Unit);release.await();HttpStatusCode.Unauthorized to """{"code":"unauthenticated","message":"synthetic"}"""}
   val write=async{runCatching{f.api.confirmExpensePair("ledger-a",pending)}}
   withTimeout(5000){started.await()};f.user="synthetic-user-b";f.api.authState();release.complete(Unit)
   val error=write.await().exceptionOrNull();assertIs<LedgerException>(error);assertEquals("expense_pair_scope_changed",error.code);assertFalse(error.accessLost);assertTrue(f.removed.isEmpty())
   f.user="synthetic-user-a";f.api.authState();assertSame(pending,f.api.pendingExpensePair("ledger-a"))
  }finally{f.api.close()}
 }
}
