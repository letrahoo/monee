package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.*
import kotlinx.coroutines.*
import kotlinx.serialization.encodeToString
import kotlin.test.*

/** Synthetic HTTP transport, not normal-login or Compose click acceptance. */
class ExpensePairEvidenceTransportTest {
 private class Fixture {
  var origin="https://original-synthetic.invalid"
  var user="synthetic-user"
  var receipt=ExpensePairReceipt("decision-a",1,"active",8,"primary-a","secondary-a")
  var relationship:ExpensePairDetail?=null
  val primary=TransactionSource("import-primary","primary.csv",2,"new")
  val secondary=TransactionSource("import-secondary","secondary.csv",3,"new")
  var includeSources=true
  var importGate:(suspend ()->Unit)?=null
  var importStatus=HttpStatusCode.OK
  val urls=mutableListOf<String>()
  val details=mutableMapOf(primary.importId to detail("primary-a",primary),secondary.importId to detail("secondary-a",secondary))
  private fun detail(transactionId:String,source:TransactionSource):ImportDetail {
   val record=LedgerTransaction("$transactionId-original","2026-09-12","expense","-1234","CNY","original $transactionId","餐饮","synthetic","synthetic account","original-source-id","original note",1)
   val document=SourceDocument("synthetic",1,listOf(SourceRow(source.line,mapOf("merchant" to record.merchant,"id" to record.externalId))))
   val preview=ImportPreview(id=source.importId,filename=source.filename,ledgerVersion=7,rows=emptyList(),newCount=1,duplicateCount=0,similarCount=0,errors=emptyList(),alreadyCommitted=true,sourceDocument=document)
   return ImportDetail(1,preview,listOf(ImportedSourceEvidence("$transactionId-evidence",source.line,transactionId,source.disposition,record)))
  }
  val client=HttpClient(MockEngine { request ->
   val path=request.url.encodedPath
   if(path=="/api/v1/auth/me")respond(apiJson.encodeToString(AuthState(AuthUser(user,"github","synthetic-sub",allowed=true),csrfToken="synthetic-csrf")))
   else {
    urls+=request.url.toString()
    assertEquals(HttpMethod.Get,request.method);assertNull(request.headers["Idempotency-Key"])
    assertEquals("ledger-a",request.headers["X-Monee-Ledger"])
    assertEquals("Bearer synthetic-token",request.headers[HttpHeaders.Authorization])
    assertEquals("synthetic-csrf",request.headers["X-Monee-CSRF"])
    when(path){
     "/api/v1/transactions/primary-a/expense-pairs","/api/v1/transactions/secondary-a/expense-pairs" -> respond(apiJson.encodeToString(relationship ?: ExpensePairDetail(9,listOf(receipt))))
     "/api/v1/transactions/primary-a/sources" -> respond(apiJson.encodeToString(if(includeSources)listOf(primary)else emptyList()))
     "/api/v1/transactions/secondary-a/sources" -> respond(apiJson.encodeToString(if(includeSources)listOf(secondary)else emptyList()))
     "/api/v1/imports/import-primary","/api/v1/imports/import-secondary" -> {
      importGate?.invoke()
      respond(apiJson.encodeToString(details.getValue(path.substringAfterLast('/'))),importStatus)
     }
     else -> error("unexpected synthetic request $path")
    }
   }
  })
  val api=LedgerApi(client,{Connection(origin)},{"synthetic-token"},{_,_->},{})
 }
 @Test fun bothRolesAndWithdrawnHistoryReadFocusedOriginalEvidenceWithoutWrites():Unit=runBlocking {
  for(withdrawn in listOf(false,true)) {
   val f=Fixture();try {
    if(withdrawn)f.receipt=f.receipt.copy(version=2,status="retracted",ledgerVersion=9)
    f.api.authState()
    val pending=f.api.prepareExpensePairConfirm("ledger-a",ExpensePairConfirmInput(9,"unrelated-a","unrelated-b",1,1,"unrelated pending")){"synthetic-unknown-key"}
    for((transactionId,source) in listOf("primary-a" to f.primary,"secondary-a" to f.secondary)){
     val result=f.api.expensePairEvidence("ledger-a",f.receipt,transactionId,source)
     assertEquals(transactionId,result.transactionId)
     assertEquals("original $transactionId",result.record.merchant)
     assertEquals("original $transactionId",result.raw?.get("merchant"))
     assertEquals(source,result.source);assertSame(pending,f.api.pendingExpensePair("ledger-a"))
    }
    assertEquals(6,f.urls.size);assertTrue(f.urls.all { it.startsWith("https://original-synthetic.invalid/") })
   }finally{f.api.close()}
  }
 }
 @Test fun missingMembershipOrChangedRelationshipStopsBeforeImportRead():Unit=runBlocking {
  for(changedReceipt in listOf(false,true)){
   val f=Fixture();try {
    f.api.authState()
    if(changedReceipt)f.relationship=ExpensePairDetail(9,listOf(f.receipt.copy(version=2,status="retracted",ledgerVersion=9))) else f.includeSources=false
    val error=assertFailsWith<LedgerException>{f.api.expensePairEvidence("ledger-a",f.receipt,"secondary-a",f.secondary)}
    assertEquals("expense_pair_contract",error.code);assertTrue(f.urls.none { "/imports/" in it })
   }finally{f.api.close()}
  }
 }
 @Test fun wrongImportOrTransactionSnapshotCannotBeDisplayed():Unit=runBlocking {
  for(wrongImport in listOf(false,true)){
   val f=Fixture();try{
    f.api.authState();val original=f.details.getValue(f.primary.importId)
    f.details[f.primary.importId]=if(wrongImport)original.copy(preview=original.preview.copy(id="different-import")) else original.copy(evidence=original.evidence.map { it.copy(transactionId="other-transaction") })
    val error=assertFailsWith<LedgerException>{f.api.expensePairEvidence("ledger-a",f.receipt,"primary-a",f.primary)}
    assertEquals("expense_pair_contract",error.code)
   }finally{f.api.close()}
  }
 }
 @Test fun invalidRoleOrImportPathIsRejectedBeforeNetwork():Unit=runBlocking{
  val f=Fixture();try {
   f.api.authState()
   assertFailsWith<IllegalArgumentException>{f.api.expensePairEvidence("ledger-a",f.receipt,"unrelated",f.primary)}
   assertFailsWith<IllegalArgumentException>{f.api.expensePairEvidence("ledger-a",f.receipt,"primary-a",f.primary.copy(importId="../escape"))}
   assertTrue(f.urls.isEmpty())
  }finally{f.api.close()}
 }
 @Test fun lateEvidenceOrUnauthorizedFromPreviousScopeReturnsNoDetails():Unit=runBlocking {
  for(lateUnauthorized in listOf(false,true)){
   val f=Fixture();try {
    f.api.authState();val started=CompletableDeferred<Unit>();val release=CompletableDeferred<Unit>()
    f.importGate={started.complete(Unit);release.await()}
    if(lateUnauthorized)f.importStatus=HttpStatusCode.Unauthorized
    val read=async{runCatching{f.api.expensePairEvidence("ledger-a",f.receipt,"secondary-a",f.secondary)}}
    withTimeout(5000){started.await()}
    f.origin="https://new-synthetic.invalid";f.user="other-synthetic-user";f.api.reconnect();f.api.authState();release.complete(Unit)
    val result=read.await();assertTrue(result.isFailure)
    val error=result.exceptionOrNull();assertIs<LedgerException>(error);assertEquals("expense_pair_scope_changed",error.code);assertFalse(error.accessLost)
    assertEquals(3,f.urls.size);assertTrue(f.urls.all { it.startsWith("https://original-synthetic.invalid/") })
   }finally{f.api.close()}
  }
 }
}
