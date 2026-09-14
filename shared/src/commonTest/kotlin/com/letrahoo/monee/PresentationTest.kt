package com.letrahoo.monee

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class PresentationTest {
    @Test
    fun moneyPreservesCentsAndLargeIntegers() {
        assertEquals("¥0.01", formatMoney(1))
        assertEquals("−¥28.50", formatMoney(-2850))
        assertEquals("¥90,071,992,547,409.93", formatMoney(9007199254740993))
        assertEquals("−¥92,233,720,368,547,758.08", formatMoney(Long.MIN_VALUE))
    }

    @Test
    fun chineseSearchRespectsSelectedMonth() {
        assertEquals(listOf("demo-002"), filterTransactions("2026-09", " 微信 ").map { it.id })
        assertEquals(listOf("demo-006"), filterTransactions("2026-08", "餐饮").map { it.id })
        assertTrue(filterTransactions("2026-09", "不存在的商户").isEmpty())
    }
}
