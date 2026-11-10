package com.letrahoo.monee.data

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import kotlin.test.*

class ExpensePairModelsTest {
    private val input=ExpensePairConfirmInput(4,"primary-a","secondary-b",2,3,"合成确认")

    @Test fun wireInputsAndReceiptsStayNarrowAndIntegerOnly() {
        val confirm=apiJson.decodeFromString<JsonObject>(apiJson.encodeToString(input))
        assertEquals(setOf("ledgerVersion","primaryTransactionId","secondaryTransactionId","primaryVersion","secondaryVersion","reason"),confirm.keys)
        val withdraw=ExpensePairWithdrawInput(5,1,"合成撤回")
        assertEquals(setOf("ledgerVersion","decisionVersion","reason"),apiJson.decodeFromString<JsonObject>(apiJson.encodeToString(withdraw)).keys)
        val receipt=apiJson.decodeFromString<ExpensePairReceipt>("""{"id":"decision-a","version":1,"status":"active","ledgerVersion":5,"primaryId":"primary-a","secondaryId":"secondary-b"}""")
        assertTrue(receipt.valid())
        assertEquals(setOf("id","version","status","ledgerVersion","primaryId","secondaryId"),apiJson.decodeFromString<JsonObject>(apiJson.encodeToString(receipt)).keys)
        assertEquals(receipt,apiJson.decodeFromString<ExpensePairReceipt>(apiJson.encodeToString(receipt)))
    }

    @Test fun previewRejectsWrongBindingsOrNonCanonicalMoney() {
        val preview=ExpensePairPreview(4,"primary-a","secondary-b",2,3,"CNY","2468","1234","-1234")
        assertTrue(preview.matches(input.previewInput()))
        for(bad in listOf(preview.copy(primaryId="other"),preview.copy(ledgerVersion=3),preview.copy(currency="USD"),
            preview.copy(pairGrossAfterMinor="01234"),preview.copy(pairGrossBeforeMinor="2468.0"),
            preview.copy(pairGrossDeltaMinor="1234"),preview.copy(pairGrossAfterMinor="1000000000001"))) {
            assertFalse(bad.matches(input.previewInput()),bad.toString())
        }
    }

    @Test fun detailMustBindEveryReceiptAndRejectDuplicateOrUnknownState() {
        val active=ExpensePairReceipt("decision-a",1,"active",5,"primary-a","secondary-b")
        assertTrue(ExpensePairDetail(8,listOf(active)).matches("secondary-b"))
        assertFalse(ExpensePairDetail(4,listOf(active)).matches("primary-a"))
        assertFalse(ExpensePairDetail(8,listOf(active,active)).matches("primary-a"))
        assertFalse(ExpensePairDetail(8,listOf(active)).matches("unrelated"))
        assertFalse(active.copy(status="unknown").valid())
        assertFalse(active.copy(version=2).valid())
    }

    @Test fun dashboardMetadataIsSeparateFromTransactionAndDefaultsEmpty() {
        val json="""{"ledgerId":"ledger-a","version":5,"month":"2026-09","today":"2026-09-15","months":[],"incomeMinor":"0","expenseMinor":"1234","sourceCount":1,"totalCount":1,"filteredCount":1,"page":1,"pageSize":50,"categories":[],"transactions":[]}"""
        val dashboard=apiJson.decodeFromString<Dashboard>(json)
        assertTrue(dashboard.expensePairs.isEmpty())
        val receipt=ExpensePairReceipt("decision-a",1,"active",5,"primary-a","secondary-b")
        assertEquals(listOf(receipt),apiJson.decodeFromString<Dashboard>(apiJson.encodeToString(dashboard.copy(expensePairs=listOf(receipt)))).expensePairs)
        val transaction=LedgerTransaction("primary-a","2026-09-15","expense","-1234","CNY","合成","餐饮","合成","合成","","",1)
        assertFalse("expensePairs" in apiJson.decodeFromString<JsonObject>(apiJson.encodeToString(transaction)))
    }
}
