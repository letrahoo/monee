package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlin.test.*

class RefundPresentationTest {
    @Test fun retryKeepsTheOriginalVersionAmountAndKey() {
        val input=RefundInput(1,"2026-09-19","30")
        val requests=RefundRequests()
        val scope=RefundScope("https://service.test","user","ledger")
        val first=requests.prepare(scope,"original",input){"stable-key"}
        // A new screen obtains the API-owned state after polling unmounted it.
        assertSame(first,requests.get(scope))
        val refreshed=requests.prepare(scope,"original",input.copy(version=2,amount="70")){error("retry generated a new key")}
        assertSame(first,refreshed)
        assertEquals(1L,refreshed.input.version)
        assertEquals("30",refreshed.input.amount)
        assertFailsWith<IllegalArgumentException>{requests.prepare(scope,"other",input){"unused"}}
        assertNull(requests.get(scope.copy(userId="other")))
        assertNull(requests.get(scope.copy(origin="https://other.test")))
        assertNull(requests.get(scope.copy(ledgerId="other")))
        requests.clear(scope,first.copy(key="wrong"))
        assertSame(first,requests.get(scope))
        requests.clear(scope,first)
        assertNull(requests.get(scope))
    }
    @Test fun chartNeverDividesByZeroOrUsesNegativeNetAsDenominator() {
        assertEquals(0f,expenseShare("0","0"))
        assertEquals(0f,expenseShare("0","-3000"))
        assertEquals(0.5f,expenseShare("5000","10000"))
        assertEquals(1f,expenseShare("10000","100"))
        assertEquals(0f,expenseShare("-100","100"))
    }
    @Test fun refundContractKeepsLinkAndNegativeNetWithoutChangingIncome() {
        val transaction="""{"id":"r","date":"2026-10-01","type":"refund","amountMinor":"3000","currency":"CNY","merchant":"合成","category":"餐饮","source":"手动退款","account":"合成","externalId":"","note":"","version":1,"refundOf":"original"}"""
        val data=apiJson.decodeFromString<Dashboard>("""{"ledgerId":"l","version":2,"month":"2026-10","today":"2026-10-01","months":["2026-10"],"incomeMinor":"0","expenseMinor":"-3000","grossExpenseMinor":"0","refundMinor":"3000","sourceCount":1,"totalCount":1,"filteredCount":1,"page":1,"pageSize":50,"categories":[{"name":"餐饮","amountMinor":"-3000","grossExpenseMinor":"0","refundMinor":"3000"}],"transactions":[$transaction]}""")
        assertEquals("original",data.transactions.single().refundOf)
        assertEquals("0",data.incomeMinor)
        assertEquals("-3000",data.expenseMinor)
        assertEquals("3000",data.categories.single().refundMinor)
        val old=apiJson.decodeFromString<CategoryTotal>("""{"name":"餐饮","amountMinor":"10000"}""")
        assertEquals("10000",old.grossExpenseMinor)
        assertEquals("0",old.refundMinor)
    }
}
