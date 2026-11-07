package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.*
import io.ktor.http.content.TextContent
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import kotlin.test.*

/** Shared wire flow only. No browser, native UI, normal login or live DB acceptance. */
class ExpensePairFlowTransportTest {
 @Test fun previewConfirmBothSourcesAndWithdrawUseCurrentDetailVersion():Unit=runBlocking {
  val input=ExpensePairConfirmInput(7,"primary-a","secondary-a",1,1,"synthetic reason")
  val preview=ExpensePairPreview(7,"primary-a","secondary-a",1,1,"CNY","2468","1234","-1234")
  val confirmed=ExpensePairReceipt("decision-a",1,"active",8,"primary-a","secondary-a")
  // Another permitted operation may have advanced the ledger after confirmation.
  val detail=ExpensePairDetail(9,listOf(confirmed))
  val withdrawn=confirmed.copy(version=2,status="retracted",ledgerVersion=10)
  val paths=mutableListOf<String>()
  val client=HttpClient(MockEngine { request ->
   val path=request.url.encodedPath;paths+=path
   if(path=="/api/v1/auth/me") respond(apiJson.encodeToString(AuthState(AuthUser("synthetic-user","github","synthetic-sub",allowed=true),csrfToken="synthetic-csrf")))
   else {
    assertEquals("ledger-a",request.headers["X-Monee-Ledger"])
    assertEquals("Bearer synthetic-token",request.headers[HttpHeaders.Authorization])
    assertEquals("synthetic-csrf",request.headers["X-Monee-CSRF"])
    when(path){
     "/api/v1/expense-pairs/preview" -> {
      assertEquals(HttpMethod.Post,request.method);assertNull(request.headers["Idempotency-Key"])
      assertEquals(input.previewInput(),apiJson.decodeFromString<ExpensePairPreviewInput>((request.body as TextContent).text))
      respond(apiJson.encodeToString(preview))
     }
     "/api/v1/expense-pairs" -> {
      assertEquals(HttpMethod.Post,request.method);assertEquals("synthetic-confirm-key",request.headers["Idempotency-Key"])
      assertEquals(input,apiJson.decodeFromString<ExpensePairConfirmInput>((request.body as TextContent).text))
      respond(apiJson.encodeToString(confirmed),HttpStatusCode.Created)
     }
     "/api/v1/transactions/primary-a/expense-pairs" -> {assertEquals(HttpMethod.Get,request.method);respond(apiJson.encodeToString(detail))}
     "/api/v1/transactions/primary-a/sources" -> respond(apiJson.encodeToString(listOf(TransactionSource("import-primary","primary.csv",2,"new"))))
     "/api/v1/transactions/secondary-a/sources" -> respond(apiJson.encodeToString(listOf(TransactionSource("import-secondary","secondary.csv",3,"new"))))
     "/api/v1/expense-pairs/decision-a/withdraw" -> {
      assertEquals(HttpMethod.Post,request.method);assertEquals("synthetic-withdraw-key",request.headers["Idempotency-Key"])
      assertEquals(ExpensePairWithdrawInput(detail.ledgerVersion,confirmed.version,"synthetic reversal"),apiJson.decodeFromString<ExpensePairWithdrawInput>((request.body as TextContent).text))
      respond(apiJson.encodeToString(withdrawn))
     }
     else -> error("unexpected synthetic path $path")
    }
   }
  })
  val api=LedgerApi(client,{Connection("https://synthetic.invalid")},{"synthetic-token"},{_,_->},{})
  try {
   api.authState();assertEquals(preview,api.previewExpensePair("ledger-a",input.previewInput()))
   val pending=api.prepareExpensePairConfirm("ledger-a",input){"synthetic-confirm-key"}
   assertEquals(confirmed,api.confirmExpensePair("ledger-a",pending));assertNull(api.pendingExpensePair("ledger-a"))
   val current=api.expensePairs("ledger-a","primary-a");assertEquals(9L,current.ledgerVersion)
   val sources=api.expensePairSources("ledger-a",confirmed)
   assertEquals("primary.csv",sources.primary.single().filename)
   assertEquals("secondary.csv",sources.secondary.single().filename)
   val retract=api.prepareExpensePairWithdraw("ledger-a",confirmed.id,ExpensePairWithdrawInput(current.ledgerVersion,confirmed.version,"synthetic reversal")){"synthetic-withdraw-key"}
   assertEquals(withdrawn,api.withdrawExpensePair("ledger-a",retract));assertNull(api.pendingExpensePair("ledger-a"))
   assertEquals(8,paths.size)
  }finally{api.close()}
 }
}
