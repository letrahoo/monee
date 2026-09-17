package com.letrahoo.monee.data

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class RedactionPreviewContractTest {
    @Test fun preservesMinorUnitStringsAndExcludedLines() {
        val result=Json.decodeFromString<RedactionPreview>("""{
          "importId":"batch","localOnly":true,"includedCount":1,
          "excluded":[{"line":6,"reason":"金额无效"}],
          "payload":{"version":"monee-redaction-v1","records":[{
            "ref":"r1_record","date":"2026-09-17","amountMinor":"9999999999999",
            "currency":"CNY","direction":"unknown","source":"wechat","identifiers":[]
          }]},"treatments":[]
        }""")
        assertTrue(result.localOnly)
        assertEquals("9999999999999",result.payload.records.single().amountMinor)
        assertEquals("unknown",result.payload.records.single().direction)
        assertEquals("",result.payload.records.single().merchantRef)
        assertEquals(6,result.excluded.single().line)
    }

    @Test fun allExcludedPayloadHasNoRecords() {
        val result=Json.decodeFromString<RedactionPreview>("""{
          "importId":"batch","localOnly":true,"includedCount":0,
          "excluded":[{"line":3,"reason":"日期无效"}],
          "payload":{"version":"monee-redaction-v1","records":[]},"treatments":[]
        }""")
        assertTrue(result.payload.records.isEmpty())
        assertEquals(0,result.includedCount)
        assertEquals(1,result.excluded.size)
    }
}
