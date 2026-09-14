package com.letrahoo.monee

import kotlin.test.Test
import kotlin.test.assertEquals
import com.letrahoo.monee.data.LedgerTransaction
import kotlinx.serialization.json.Json

class PresentationTest {
    @Test
    fun moneyPreservesCentsAndLargeIntegers() {
        assertEquals("¥0.01", formatMoney(1))
        assertEquals("−¥28.50", formatMoney(-2850))
        assertEquals("¥90,071,992,547,409.93", formatMoney(9007199254740993))
        assertEquals("−¥92,233,720,368,547,758.08", formatMoney(Long.MIN_VALUE))
    }

    @Test
    fun serverMoneyRemainsExactThroughJson() {
        val record = Json.decodeFromString<LedgerTransaction>("""{"id":"test","date":"2026-09-14","type":"income","amountMinor":"9007199254740993","currency":"CNY","merchant":"中文商户","category":"工资","source":"银行","account":"待核实","externalId":"","note":"","version":1}""")
        assertEquals("9007199254740993", record.amountMinor)
        assertEquals("¥90,071,992,547,409.93", formatMoney(record.amountMinor.toLong()))
    }
}
