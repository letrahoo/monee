package com.letrahoo.monee

import com.letrahoo.monee.data.*
import kotlin.test.*

class CorrectionInputTest {
    @Test fun decimalInputNeverRoundsOrUsesFloatingPoint() {
        assertEquals("1",correctionMinorInput("0.01"))
        assertEquals("1230",correctionMinorInput("12.3"))
        assertEquals("9007199254740991",correctionMinorInput("90071992547409.91"))
        listOf("0","-12.3","1.001","1e3","1,000.00","NaN","92233720368547758.08").forEach { assertNull(correctionMinorInput(it),it) }
    }
    @Test fun signedServerImpactRemainsExact() {
        val transaction="""{"id":"x","date":"2026-09-17","type":"expense","amountMinor":"-1234","currency":"CNY","merchant":"synthetic","category":"food","source":"test","account":"test","externalId":"","note":"","version":1}"""
        val preview=apiJson.decodeFromString<CorrectionPreview>("""{"before":$transaction,"after":${transaction.replace("\"-1234\"","\"-1500\"")},"incomeDeltaMinor":"0","expenseDeltaMinor":"266","netDeltaMinor":"-266"}""")
        assertEquals("266",preview.expenseDeltaMinor)
        assertEquals("-266",preview.netDeltaMinor)
        assertEquals("-1500",preview.after.amountMinor)
    }
}
