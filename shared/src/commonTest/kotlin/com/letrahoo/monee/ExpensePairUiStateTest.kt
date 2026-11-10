package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlin.test.*

class ExpensePairUiStateTest {
    private fun transaction(id:String,version:Long=1,type:String="expense")=LedgerTransaction(
        id,"2026-09-12",type,"-1234","CNY","synthetic merchant","餐饮","synthetic source","synthetic account","","",version)
    private val receipt=ExpensePairReceipt("decision-a",1,"active",8,"primary-a","secondary-a")
    private fun dashboard(version:Long=7,transactions:List<LedgerTransaction> = listOf(transaction("primary-a"),transaction("secondary-a")),
        pairs:List<ExpensePairReceipt> = emptyList())=Dashboard(
        ledgerId="ledger-a",version=version,month="2026-09",today="2026-09-12",months=listOf("2026-09"),
        incomeMinor="0",expenseMinor="2468",sourceCount=1,totalCount=transactions.size,filteredCount=transactions.size,
        page=1,pageSize=50,categories=emptyList(),transactions=transactions,expensePairs=pairs)
    private fun ticket(data:Dashboard=dashboard(),primary:String?="primary-a",secondary:String?="secondary-a",reason:String="synthetic reason",
        readOnly:Boolean=false,pending:ExpensePairSubmission?=null)=expensePairUiPreviewTicket(
        expensePairUiSnapshot(data,"ledger-a"),primary,secondary,reason,readOnly,pending)
    private fun pending()=ExpensePairConfirmSubmission(ExpensePairScope("https://synthetic.invalid","user-a","ledger-a"),
        ExpensePairConfirmInput(7,"primary-a","secondary-a",1,1,"synthetic reason"),"synthetic-original-key")

    @Test fun neverGuessesPrimaryOrSecondaryAndRequiresReason() {
        assertNull(ticket(primary=null));assertNull(ticket(secondary=null));assertNull(ticket(primary="primary-a",secondary="primary-a"))
        assertNull(ticket(reason="  "));assertNull(ticket(primary="outside-this-page"))
        val result=assertNotNull(ticket())
        assertEquals("primary-a",result.input.primaryTransactionId)
        assertEquals("secondary-a",result.input.secondaryTransactionId)
        assertEquals(7L,result.input.ledgerVersion)
        assertEquals("synthetic reason",result.confirmInput().reason)
    }

    @Test fun currentPageFiltersAssociatedMembersAndNonExpenses() {
        val data=dashboard(transactions=listOf(transaction("primary-a"),transaction("secondary-a"),
            transaction("income-a",type="income"),transaction("other-expense")),pairs=listOf(receipt))
        assertEquals(listOf("other-expense"),expensePairUiCandidates(data,"ledger-a").map { it.id })
        assertTrue(expensePairUiCandidates(data,"other-ledger").isEmpty())
        assertNull(expensePairUiSnapshot(data,"other-ledger"))
        assertNull(ticket(data))
        val withdrawn=data.copy(expensePairs=listOf(receipt.copy(status="retracted",version=2)))
        assertEquals(listOf("primary-a","secondary-a","other-expense"),expensePairUiCandidates(withdrawn,"ledger-a").map { it.id })
    }

    @Test fun selectionReasonCandidateAndVersionChangesExpirePreview() {
        val checked=assertNotNull(ticket())
        val original=expensePairUiSnapshot(dashboard(),"ledger-a")
        assertTrue(checked.current(original,"primary-a","secondary-a","synthetic reason"))
        assertFalse(checked.current(original,"secondary-a","primary-a","synthetic reason"))
        assertFalse(checked.current(original,"primary-a","secondary-a","changed reason"))
        assertFalse(checked.current(expensePairUiSnapshot(dashboard(version=8),"ledger-a"),"primary-a","secondary-a","synthetic reason"))
        val changed=dashboard(transactions=listOf(transaction("primary-a",2),transaction("secondary-a")))
        assertFalse(checked.current(expensePairUiSnapshot(changed,"ledger-a"),"primary-a","secondary-a","synthetic reason"))
        val removed=dashboard(transactions=listOf(transaction("primary-a")))
        assertFalse(checked.current(expensePairUiSnapshot(removed,"ledger-a"),"primary-a","secondary-a","synthetic reason"))
        assertFalse(checked.current(null,"primary-a","secondary-a","synthetic reason"))
    }

    @Test fun viewerAndUnknownResultLockAllNewWrites() {
        val pending=pending()
        assertNull(ticket(readOnly=true));assertNull(ticket(pending=pending))
        assertFalse(expensePairUiCanWrite(true,false,null))
        assertFalse(expensePairUiCanWrite(false,true,null))
        assertFalse(expensePairUiCanWrite(false,false,pending))
        assertFalse(expensePairUiCanWrite(false,false,null,false))
        assertTrue(expensePairUiPendingMatches(pending,"user-a","ledger-a"))
        assertFalse(expensePairUiPendingMatches(pending,"user-b","ledger-a"))
        assertFalse(expensePairUiPendingMatches(pending,"user-a","ledger-b"))
        // UI keeps the registration, never recreating it from its display fields.
        val restored:ExpensePairSubmission=pending
        assertSame(pending,restored)
    }

    @Test fun detailResultsExpireOnTargetVersionOrGenerationChange() {
        val ticket=ExpensePairUiDetailTicket("primary-a",9,3)
        assertTrue(ticket.current("primary-a",9,3))
        assertFalse(ticket.current("secondary-a",9,3))
        assertFalse(ticket.current("primary-a",10,3))
        assertFalse(ticket.current("primary-a",9,4))
        assertFalse(ticket.current(null,9,3))
    }

    @Test fun withdrawUsesCurrentDetailAndRequiresExplicitConsent() {
        // Receipt's historical version8 is not the current detail version9.
        val detail=ExpensePairDetail(9,listOf(receipt))
        assertTrue(expensePairUiCanWithdraw(detail,9,receipt,false,false,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,10,receipt,false,false,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,9,receipt,true,false,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,9,receipt,false,true,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,9,receipt,false,false,pending(),"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,9,receipt,false,false,null,"",true))
        assertFalse(expensePairUiCanWithdraw(detail,9,receipt,false,false,null,"synthetic correction",false))
        assertFalse(expensePairUiCanWithdraw(null,9,receipt,false,false,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(detail,null,receipt,false,false,null,"synthetic correction",true))
        assertFalse(expensePairUiCanWithdraw(ExpensePairDetail(10,listOf(receipt.copy(status="retracted",version=2))),10,
            receipt,false,false,null,"synthetic correction",true))
    }

    @Test fun amountsAndErrorsStayExplicit() {
        assertEquals(formatMoney(-1234),expensePairUiMoney("-1234"))
        assertEquals("invalid-amount",expensePairUiMoney("invalid-amount"))
        assertTrue(expensePairUiError(LedgerException("synthetic","expense_pair_not_enabled")).contains("尚未启用"))
        assertTrue(expensePairUiError(LedgerException("synthetic","expense_pair_scope_changed")).contains("原账号"))
        val key=expensePairUiRequestKey()
        assertEquals(32,key.length);assertTrue(key.all { it in '0'..'9' || it in 'a'..'f' })
    }
}
