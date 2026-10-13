package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlinx.serialization.encodeToString
import kotlin.test.*

class TransferPresentationTest {
    @Test fun uncertainTransferKeepsBothAccountsAmountAndKeyAcrossScreens() {
        val requests=TransferRequests()
        val scope=TransferScope("https://service.test","user","ledger")
        val input=TransferInput("2026-09-19","100.01","银行","钱包",note="本人转账")
        val first=requests.prepare(scope,input){"stable-key"}
        assertSame(first,requests.get(scope))
        assertSame(first,requests.prepare(scope,input.copy(amount="900",toAccount="其他")){error("retry replaced key")})
        assertEquals(input,requests.get(scope)!!.input)
        for(other in listOf(scope.copy(origin="https://other.test"),scope.copy(userId="other"),scope.copy(ledgerId="other"))) {
            assertNull(requests.get(other))
            val separate=requests.prepare(other,input){"separate-key"}
            requests.clear(other,separate)
            assertSame(first,requests.get(scope))
        }
        requests.clear(scope,first.copy(key="wrong"))
        assertSame(first,requests.get(scope))
        requests.clear(scope,first)
        assertNull(requests.get(scope))
        assertEquals("new-key",requests.prepare(scope,input){"new-key"}.key)
    }

    @Test fun twoExplicitDifferentAccountsAreRequired() {
        assertTrue(transferAccountsValid(" 银行 "," 钱包 "))
        for(pair in listOf("" to "钱包","银行" to " ","银行 " to " 银行","待核实资金账户" to "钱包","银行" to "待核实资金账户")) {
            assertFalse(transferAccountsValid(pair.first,pair.second))
        }
    }

    @Test fun transferContractIsNeutralAndPreservesDirection() {
        val raw="""{"id":"t","date":"2026-09-19","type":"transfer","amountMinor":"10001","currency":"CNY","merchant":"本人账户转账","category":"本人转账","source":"手动转账","account":"银行","toAccount":"钱包","externalId":"","note":"","version":1}"""
        val t=apiJson.decodeFromString<LedgerTransaction>(raw)
        assertEquals("转账 ¥100.01",transactionAmountLabel(t))
        assertEquals("银行 → 钱包",transactionAccountLabel(t))
        assertEquals("",t.refundOf)
        val d=apiJson.decodeFromString<Dashboard>("""{"ledgerId":"l","version":2,"month":"2026-09","today":"2026-09-19","months":["2026-09"],"incomeMinor":"0","expenseMinor":"0","grossExpenseMinor":"0","refundMinor":"0","sourceCount":1,"totalCount":1,"filteredCount":1,"page":1,"pageSize":50,"categories":[],"transactions":[$raw]}""")
        assertEquals("0",d.incomeMinor)
        assertEquals("0",d.expenseMinor)
        assertTrue(d.categories.isEmpty())
        val old=apiJson.decodeFromString<LedgerTransaction>(raw.replace(",\"toAccount\":\"钱包\"","").replace("\"transfer\"","\"income\""))
        assertEquals("",old.toAccount)
        assertEquals("+¥100.01",transactionAmountLabel(old))
        assertEquals("−¥100.01",transactionAmountLabel(old.copy(type="expense",amountMinor="-10001")))
        val input=TransferInput("2026-09-19","100.01","银行","钱包")
        assertEquals(input,apiJson.decodeFromString<TransferInput>(apiJson.encodeToString(input)))
    }
}
