package com.letrahoo.monee.data

import kotlin.test.*

class AssociationHintsControllerIntegrationTest {
    private val scope=AssociationQueryScope("synthetic-user", "synthetic-ledger", "base", 1, 2)
    private fun page(number:Int=1,total:Int=1,items:List<AssociationCandidate> = listOf(
        AssociationCandidate(AssociationTransaction("other",1,"2026-09-14","expense","-1234","CNY","合成商户","来源B"),listOf("current_projection_match","different_source_display"))
    ))=AssociationCandidates(1,true,"current_transaction_similarity",false,emptyList(),2,
        AssociationTransaction("base",1,"2026-09-14","expense","-1234","CNY","合成商户","来源A"),items,number,50,total)

    @Test fun onlyLatestRequestCanUpdateVisibleState() {
        val controller=AssociationHintsController(scope)
        val old=controller.begin(1)
        val latest=controller.begin(1)
        val beforeOldResult=controller.state
        assertFalse(controller.complete(old,page()))
        assertEquals(beforeOldResult,controller.state)
        assertTrue(controller.state.loading)
        assertNull(controller.state.data)
        val beforeOldError=controller.state
        assertFalse(controller.fail(old,LedgerException("synthetic old access loss","access_denied")))
        assertEquals(beforeOldError,controller.state)
        assertTrue(controller.state.loading)
        controller.complete(latest,page())
        assertFalse(controller.state.loading)
        assertEquals("other",controller.state.data?.items?.single()?.transaction?.id)
    }
    @Test fun closeCancelAndRetryCannotRestoreAnOldPage() {
        val controller=AssociationHintsController(scope)
        val first=controller.begin(1);controller.complete(first,page())
        val next=controller.begin(2)
        assertNull(controller.state.data)
        controller.fail(next,LedgerException("synthetic connection failure"))
        assertNull(controller.state.data)
        assertFalse(controller.state.loading)
        val retry=controller.begin(1)
        assertNull(controller.state.error)
        controller.clear()
        assertFalse(controller.complete(retry,page()))
        assertNull(controller.state.data)
        assertFalse(controller.state.loading)
        val canceled=controller.begin(1);controller.cancel(canceled)
        assertFalse(controller.complete(canceled,page()))
        assertNull(controller.state.data)
    }
    @Test fun changingLedgerVersionNeverCombinesPages() {
        val controller=AssociationHintsController(scope)
        val first=controller.begin(1);controller.complete(first,page())
        val next=controller.begin(2)
        controller.complete(next,page(number=2,total=51).copy(ledgerVersion=3))
        assertTrue(controller.state.stale)
        assertNull(controller.state.data)
        assertFalse(controller.state.loading)
    }
    @Test fun readonlyContractAndShapeAreFailClosed() {
        val changes=listOf<(AssociationCandidates)->AssociationCandidates>(
            {it.copy(contractVersion=2)}, {it.copy(readOnly=false)}, {it.copy(sourceRolesVerified=true)},
            {it.copy(actions=listOf("confirm"))}, {it.copy(basis="verified_bank_pair")},
            {it.copy(page=2)}, {it.copy(pageSize=10)}, {it.copy(totalCount=0)},
            {it.copy(base=it.base.copy(id="another-base"))},
            {it.copy(items=listOf(it.items.single().copy(transaction=it.items.single().transaction.copy(amountMinor="invalid"))))},
            {it.copy(items=listOf(it.items.single().copy(transaction=it.items.single().transaction.copy(id="base"))))},
            {it.copy(items=listOf(it.items.single().copy(transaction=it.items.single().transaction.copy(sourceDisplay="来源A"))))},
            {it.copy(items=listOf(it.items.single().copy(reasons=listOf("confirmed"))))},
            {it.copy(items=it.items+it.items,totalCount=2)}
        )
        changes.forEach { change->
            val controller=AssociationHintsController(scope)
            val request=controller.begin(1)
            controller.complete(request,change(page()))
            assertNull(controller.state.data,"Bad contract must never leave visible items")
            assertFalse(controller.state.loading)
            assertNotNull(controller.state.error)
        }
    }
    @Test fun baseVersionAndEmptyResultsHaveDistinctMeaning() {
        val controller=AssociationHintsController(scope)
        val request=controller.begin(1)
        controller.complete(request,page().copy(base=page().base.copy(version=2)))
        assertTrue(controller.state.stale)
        assertNull(controller.state.data)
        val fresh=AssociationHintsController(scope)
        fresh.complete(fresh.begin(1),page(total=0,items=emptyList()))
        assertEquals(0,fresh.state.data?.totalCount)
        assertTrue(fresh.state.data?.items?.isEmpty()==true)
        assertNull(fresh.state.error)
    }
    @Test fun validNextPageReplacesRatherThanAppendsAndPermissionErrorClearsIt() {
        val controller=AssociationHintsController(scope)
        val template=page().items.single()
        val first=(0 until 50).map {template.copy(transaction=template.transaction.copy(id="other-${it.toString().padStart(3,'0')}"))}
        controller.complete(controller.begin(1),page(total=51,items=first))
        assertEquals(50,controller.state.data?.items?.size)
        val final=template.copy(transaction=template.transaction.copy(id="other-050"))
        controller.complete(controller.begin(2),page(number=2,total=51,items=listOf(final)))
        assertEquals(listOf("other-050"),controller.state.data?.items?.map {it.transaction.id})
        val denied=controller.begin(1)
        assertTrue(controller.fail(denied,LedgerException("synthetic denied","ledger_denied")))
        assertNull(controller.state.data)
        assertEquals("ledger_denied",controller.state.errorCode)
    }
    @Test fun independentContextCannotExposePreviousControllerData() {
        val old=AssociationHintsController(scope)
        old.complete(old.begin(1),page())
        val next=AssociationHintsController(scope.copy(userId="another-user",ledgerId="another-ledger",transactionId="another-base"))
        assertNull(next.state.data)
        assertFalse(next.state.loading)
        next.complete(next.begin(1),page())
        assertNull(next.state.data)
        assertEquals("other",old.state.data?.items?.single()?.transaction?.id)
    }
}
