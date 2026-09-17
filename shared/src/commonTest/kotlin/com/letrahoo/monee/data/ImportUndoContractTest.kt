package com.letrahoo.monee.data

import kotlin.test.*

class ImportUndoContractTest {
    @Test fun historyKeepsWithdrawnStateAndOriginalReceipt() {
        val history=apiJson.decodeFromString<ImportHistory>("""{"imports":[{"id":"i","filename":"synthetic.csv","createdAt":"2026-09-17","committedAt":"2026-09-17","undone":true,"result":{"importId":"i","added":2,"skipped":0,"month":"2026-09"}}],"page":1,"pageSize":50,"totalCount":1}""")
        assertTrue(history.imports.single().undone)
        assertEquals(2,history.imports.single().result!!.added)
    }
    @Test fun restoreConflictIsExposedEvenWhenSomeRowsRemainRestorable() {
        val preview=apiJson.decodeFromString<ImportUndoPreview>("""{"importId":"i","ledgerVersion":7,"state":"undone","action":"restore","changeCount":1,"preservedCount":1,"blockedCount":1,"incomeMinor":"0","expenseMinor":"1234","alreadyApplied":false,"rows":[]}""")
        assertEquals(1,preview.blockedCount)
        assertEquals(1,preview.changeCount)
    }
    @Test fun preservedTransactionsAreDistinctFromImpactedTotals() {
        val preview=apiJson.decodeFromString<ImportUndoPreview>("""{"importId":"i","ledgerVersion":4,"state":"committed","action":"undo","changeCount":1,"preservedCount":1,"incomeMinor":"0","expenseMinor":"1234","alreadyApplied":false,"rows":[{"transactionId":"edited","date":"2026-09-17","merchant":"synthetic","type":"income","amountMinor":"1500","action":"preserve","reason":"edited"}]}""")
        assertEquals("1234",preview.expenseMinor)
        assertEquals("preserve",preview.rows.single().action)
        assertFalse(preview.alreadyApplied)
    }
}
