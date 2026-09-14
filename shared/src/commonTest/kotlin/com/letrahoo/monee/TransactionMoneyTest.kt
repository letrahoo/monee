package com.letrahoo.monee

import kotlin.test.Test
import kotlin.test.assertEquals

class TransactionMoneyTest {
    @Test fun transactionDirectionAppearsOnce() {
        assertEquals("−¥89.00", formatTransactionMoney(-8900))
        assertEquals("+¥89.00", formatTransactionMoney(8900))
        assertEquals("¥0.00", formatTransactionMoney(0))
        assertEquals("−¥92,233,720,368,547,758.08", formatTransactionMoney(Long.MIN_VALUE))
    }
}
