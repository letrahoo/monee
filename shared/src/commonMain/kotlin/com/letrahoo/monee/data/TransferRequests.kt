package com.letrahoo.monee.data

internal data class TransferScope(val origin:String,val userId:String,val ledgerId:String)

// Long-lived API ownership preserves uncertain writes across panel and screen
// recreation. Never replace their input/key until the server resolves the result.
internal class TransferRequests {
    private val pending=mutableMapOf<TransferScope,TransferSubmission>()
    fun get(scope:TransferScope):TransferSubmission?=pending[scope]
    fun prepare(scope:TransferScope,input:TransferInput,newKey:()->String):TransferSubmission =
        pending[scope] ?: TransferSubmission(input,newKey()).also { pending[scope]=it }
    fun clear(scope:TransferScope,submitted:TransferSubmission) {
        if(pending[scope]==submitted)pending.remove(scope)
    }
}
