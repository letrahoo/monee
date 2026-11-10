package com.letrahoo.monee.data

import kotlin.test.*

class ExpensePairRequestsTest {
    private val scope=ExpensePairScope("https://synthetic.invalid","user-a","ledger-a")
    private val input=ExpensePairConfirmInput(4,"primary-a","secondary-b",1,1,"合成确认")

    @Test fun uncertainConfirmationRetainsExactInputAndBlocksOtherOperation() {
        val requests=ExpensePairRequests()
        val first=requests.prepareConfirm(scope,input){"synthetic-stable-key"}
        assertEquals(scope,first.scope)
        assertSame(first,requests.prepareConfirm(scope,input.copy(reason="changed")){error("replaced uncertain key")})
        assertFailsWith<LedgerException>{requests.prepareWithdraw(scope,"decision-a",ExpensePairWithdrawInput(5,1,"合成撤回")){"synthetic-other-key"}}
        assertSame(first,requests.get(scope))
        requests.clear(scope,first.copy())
        assertSame(first,requests.get(scope))
        requests.clear(scope,first)
        assertNull(requests.get(scope))
    }

    @Test fun pendingIsIsolatedByOriginUserLedgerAndOperationRemainsVisible() {
        val requests=ExpensePairRequests()
        val first=requests.prepareWithdraw(scope,"decision-a",ExpensePairWithdrawInput(5,1,"合成撤回")){"synthetic-withdraw-key"}
        assertEquals("decision-a",first.decisionId)
        for(other in listOf(scope.copy(origin="https://other.invalid"),scope.copy(userId="user-b"),scope.copy(ledgerId="ledger-b"))) {
            assertNull(requests.get(other))
            val separate=requests.prepareConfirm(other,input){"synthetic-separate-key"}
            requests.clear(other,separate)
            assertSame(first,requests.get(scope))
        }
        assertSame(first,requests.prepareWithdraw(scope,"decision-b",ExpensePairWithdrawInput(99,1,"changed")){error("replaced uncertain withdrawal")})
    }

    @Test fun responseMustMatchOriginalSubmission() {
        val requests=ExpensePairRequests()
        val submission=requests.prepareConfirm(scope,input){"synthetic-confirm-key"}
        val receipt=ExpensePairReceipt("decision-a",1,"active",5,"primary-a","secondary-b")
        assertTrue(receipt.matches(submission))
        assertFalse(receipt.copy(ledgerVersion=6).matches(submission))
        assertFalse(receipt.copy(secondaryId="unrelated").matches(submission))
        requests.clear(scope,submission)
        val withdrawal=requests.prepareWithdraw(scope,"decision-a",ExpensePairWithdrawInput(8,1,"合成撤回")){"synthetic-withdraw-key"}
        assertTrue(receipt.copy(version=2,status="retracted",ledgerVersion=9).matches(withdrawal))
        assertFalse(receipt.copy(id="decision-b",version=2,status="retracted",ledgerVersion=9).matches(withdrawal))
    }
}
