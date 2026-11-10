package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import kotlinx.coroutines.*
import kotlinx.serialization.encodeToString
import kotlin.test.*

class ExpensePairSourcesTransportTest {
 private val receipt=ExpensePairReceipt("decision-a",1,"active",8,"primary-a","secondary-a")
 @Test fun reconnectBetweenSourcesReturnsNoMixedResultAndNeverReadsSecondOrigin():Unit=runBlocking {
  var origin="https://original-synthetic.invalid"
  val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
  val urls=mutableListOf<String>()
  val client=HttpClient(MockEngine { request ->
   when(request.url.encodedPath){
    "/api/v1/auth/me" -> respond(apiJson.encodeToString(AuthState(AuthUser("synthetic-user","github","synthetic-sub",allowed=true),csrfToken="synthetic-csrf")))
    "/api/v1/transactions/primary-a/expense-pairs" -> {urls+=request.url.toString();respond(apiJson.encodeToString(ExpensePairDetail(9,listOf(receipt))))}
    "/api/v1/transactions/primary-a/sources" -> {urls+=request.url.toString();started.complete(Unit);release.await();respond(apiJson.encodeToString(listOf(TransactionSource("synthetic-primary","primary.csv",2,"new"))))}
    else -> error("secondary or new origin read must not happen: ${request.url}")
   }
  })
  val api=LedgerApi(client,{Connection(origin)},{"synthetic-token"},{_,_->},{})
  try {
   api.authState()
   val read=async{runCatching{api.expensePairSources("ledger-a",receipt)}}
   withTimeout(5000){started.await()}
   origin="https://new-synthetic.invalid";api.reconnect();api.authState();release.complete(Unit)
   val result=read.await();assertTrue(result.isFailure)
   val error=result.exceptionOrNull();assertIs<LedgerException>(error);assertEquals("expense_pair_scope_changed",error.code)
   assertEquals(2,urls.size);assertTrue(urls.all { it.startsWith("https://original-synthetic.invalid/") })
  }finally{api.close()}
 }
 @Test fun changedRelationshipStopsBeforeEitherSourceRead():Unit=runBlocking {
  var sourceReads=0
  val client=HttpClient(MockEngine { request ->
   when(request.url.encodedPath){
    "/api/v1/auth/me" -> respond(apiJson.encodeToString(AuthState(AuthUser("synthetic-user","github","synthetic-sub",allowed=true),csrfToken="synthetic-csrf")))
    "/api/v1/transactions/primary-a/expense-pairs" -> respond(apiJson.encodeToString(ExpensePairDetail(9,listOf(receipt.copy(version=2,status="retracted",ledgerVersion=9)))))
    else -> {sourceReads++;error("source read before receipt binding")}
   }
  })
  val api=LedgerApi(client,{Connection("https://synthetic.invalid")},{null},{_,_->},{})
  try {api.authState();val error=assertFailsWith<LedgerException>{api.expensePairSources("ledger-a",receipt)};assertEquals("expense_pair_contract",error.code);assertEquals(0,sourceReads)}finally{api.close()}
 }
}
