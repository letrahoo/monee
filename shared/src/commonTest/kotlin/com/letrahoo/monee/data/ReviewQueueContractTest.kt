package com.letrahoo.monee.data

import kotlin.test.*

class ReviewQueueContractTest {
    @Test fun pendingAmountAndEvidenceRemainSourceText() {
        val queue=apiJson.decodeFromString<ReviewQueue>("""{"items":[{"id":"batch:5","importId":"batch","filename":"synthetic.csv","line":5,"format":"alipay","batchState":"undone","date":"unrecognized-date","merchant":"synthetic","amount":"1.001","reason":"invalid precision","raw":{"金额":"1.001","订单号":"00001234"}}],"page":1,"pageSize":50,"totalCount":1,"format":"alipay"}""")
        val item=queue.items.single()
        assertEquals("1.001",item.amount)
        assertEquals("00001234",item.raw["订单号"])
        assertEquals("undone",item.batchState)
        assertEquals("unrecognized-date",item.date)
    }
    @Test fun emptyFilterResultIsStillAnExplicitPage() {
        val queue=apiJson.decodeFromString<ReviewQueue>("""{"items":[],"page":1,"pageSize":50,"totalCount":0,"format":"wechat"}""")
        assertEquals("wechat",queue.format)
        assertEquals(0,queue.totalCount)
        assertTrue(queue.items.isEmpty())
    }
}
