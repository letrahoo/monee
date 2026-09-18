package com.letrahoo.monee.data

internal data class RefundScope(val origin:String,val userId:String,val ledgerId:String)

// Owned by the long-lived LedgerApi, never by a screen which polling can unmount.
// Contains only this running client's uncertain writes; it is not a ledger cache.
internal class RefundRequests {
    private val pending=mutableMapOf<RefundScope,RefundSubmission>()
    fun get(scope:RefundScope):RefundSubmission?=pending[scope]
    fun prepare(scope:RefundScope,originalId:String,input:RefundInput,newKey:()->String):RefundSubmission {
        val previous=pending[scope]
        require(previous==null||previous.originalId==originalId){"请先确认上一笔退款结果"}
        return previous ?: RefundSubmission(originalId,input,newKey()).also { pending[scope]=it }
    }
    fun clear(scope:RefundScope,submitted:RefundSubmission) {
        if(pending[scope]==submitted)pending.remove(scope)
    }
}
