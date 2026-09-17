package com.letrahoo.monee.data

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ImportContractTest {
    @Test fun nativeReviewKeepsExactAmountAndAccount() {
        val result=apiJson.decodeFromString<ImportPreview>("""{"id":"batch","filename":"a.csv","ledgerVersion":1,"rows":[],"newCount":0,"duplicateCount":0,"similarCount":0,"errors":[],"alreadyCommitted":false,"sourceAccount":"wallet-a","pending":[{"line":3,"date":"2026-09-15","merchant":"商户","amount":"12.345","reason":"金额精度需核实"}],"sourceDocument":{"parser":"alipay-csv","parserVersion":1,"records":[{"line":3,"raw":{"金额":"12.345"}}]}}""")
        assertEquals("12.345",result.pending.single().amount)
        assertEquals("wallet-a",result.sourceAccount)
        assertTrue(result.rows.isEmpty())
        assertEquals("12.345",result.sourceDocument!!.records.single().raw["金额"])
    }
    @Test fun legacyCommitAndHistoryStillDecode() {
        val result=apiJson.decodeFromString<CommitResult>("""{"importId":"a","added":1,"skipped":0,"month":"2026-09"}""")
        assertEquals(0,result.pending)
        val history=apiJson.decodeFromString<ImportHistory>("""{"imports":[{"id":"a","filename":"a.csv","createdAt":"2026-09-15","committedAt":null,"result":null}],"page":1,"pageSize":50,"totalCount":1}""")
        assertEquals(null,history.imports.single().result)
    }
}
