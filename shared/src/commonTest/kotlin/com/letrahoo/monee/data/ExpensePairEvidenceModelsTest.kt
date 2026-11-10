package com.letrahoo.monee.data

import kotlinx.serialization.encodeToString
import kotlin.test.*

class ExpensePairEvidenceModelsTest {
 private val source=TransactionSource("import-a","synthetic.csv",2,"new")
 private val record=LedgerTransaction("original-record","2026-09-12","expense","-1234","CNY","original merchant","餐饮","synthetic source","synthetic account","original-id","original note",1)
 private val original=ImportedSourceEvidence("evidence-a",2,"primary-a","new",record)
 private fun detail():ImportDetail {
  val raw=SourceDocument("synthetic",1,listOf(SourceRow(2,mapOf("merchant" to "original merchant","source-id" to "original-id"))))
  val preview=ImportPreview(id="import-a",filename="synthetic.csv",ledgerVersion=7,rows=emptyList(),newCount=1,duplicateCount=0,similarCount=0,errors=emptyList(),alreadyCommitted=true,sourceDocument=raw)
  return ImportDetail(1,preview,listOf(original))
 }

 @Test fun presentsOriginalProvenanceWithoutReplacingTransactionIdentity() {
  val result=assertNotNull(detail().expensePairEvidence("primary-a",source))
  assertSame(record,result.record)
  assertEquals("primary-a",result.transactionId)
  assertEquals("original-record",result.record.id) // Snapshot ID need not be the linked live ID.
  assertEquals(mapOf("merchant" to "original merchant","source-id" to "original-id"),result.raw)
 }
 @Test fun wrongImportLineTransactionOrDispositionCannotSelectOtherEvidence() {
  val d=detail()
  assertNull(d.expensePairEvidence("secondary-a",source))
  assertNull(d.expensePairEvidence("primary-a",source.copy(importId="other-import")))
  assertNull(d.expensePairEvidence("primary-a",source.copy(line=3)))
  assertNull(d.expensePairEvidence("primary-a",source.copy(disposition="duplicate")))
  assertNull(d.copy(evidence=listOf(original,original.copy(id="duplicate-evidence"))).expensePairEvidence("primary-a",source))
  assertNull(d.copy(evidence=emptyList()).expensePairEvidence("primary-a",source))
 }
 @Test fun redactedOriginalFieldsDoNotFabricateRawTextOrHideNormalizedEvidence() {
  val d=detail()
  val redacted=d.copy(preview=d.preview.copy(sourceDocument=null))
  val result=assertNotNull(redacted.expensePairEvidence("primary-a",source))
  assertNull(result.raw);assertSame(record,result.record)
  val ambiguous=d.copy(preview=d.preview.copy(sourceDocument=d.preview.sourceDocument!!.copy(records=listOf(SourceRow(2),SourceRow(2)))) )
  assertNull(ambiguous.expensePairEvidence("primary-a",source))
 }
 @Test fun wireEvidenceIsOptionalForOldImportReadersButRequiredForFocusedDisplay() {
  val full=apiJson.encodeToString(detail())
  assertEquals(detail(),apiJson.decodeFromString<ImportDetail>(full))
  val old="""{"parserVersion":1,"preview":${apiJson.encodeToString(detail().preview)}}"""
  val decoded=apiJson.decodeFromString<ImportDetail>(old)
  assertTrue(decoded.evidence.isEmpty());assertNull(decoded.expensePairEvidence("primary-a",source))
 }
}
