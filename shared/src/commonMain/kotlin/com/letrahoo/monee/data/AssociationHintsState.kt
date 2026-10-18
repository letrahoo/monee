package com.letrahoo.monee.data

internal data class AssociationQueryScope(
    val userId:String,
    val ledgerId:String,
    val transactionId:String,
    val transactionVersion:Long,
    val ledgerVersion:Long,
)

internal data class AssociationPageRequest(
    val scope:AssociationQueryScope,
    val page:Int,
    val generation:Long,
)

internal data class AssociationHintsState(
    val scope:AssociationQueryScope,
    val request:AssociationPageRequest?=null,
    val data:AssociationCandidates?=null,
    val error:String?=null,
    val errorCode:String?=null,
    val stale:Boolean=false,
) {
    val loading:Boolean get()=request!=null
}

// A UI-owned, non-Compose state machine. Pages replace one another, never merge;
// every new scope must own a new controller. Old completions have no authority.
internal class AssociationHintsController(scope:AssociationQueryScope) {
    var state=AssociationHintsState(scope)
        private set
    private var generation=0L

    fun begin(page:Int=1):AssociationPageRequest {
        require(page in 1..1_000_000) { "分页参数无效" }
        generation++
        val request=AssociationPageRequest(state.scope,page,generation)
        state=AssociationHintsState(state.scope,request=request)
        return request
    }

    fun complete(request:AssociationPageRequest,page:AssociationCandidates):Boolean {
        if(!current(request))return false
        state=when(validateAssociationCandidates(page,state.scope,request.page)) {
            AssociationValidation.Valid -> AssociationHintsState(state.scope,data=page)
            AssociationValidation.Stale -> AssociationHintsState(state.scope,error="交易或账本已变化，请刷新后重新排查",errorCode="association_stale",stale=true)
            AssociationValidation.Invalid -> AssociationHintsState(state.scope,error="相似提示响应无效，请刷新后重试",errorCode="association_invalid")
        }
        return true
    }

    fun fail(request:AssociationPageRequest,error:LedgerException):Boolean {
        if(!current(request))return false
        val stale=error.code in listOf("association_stale","association_scope_changed")
        val message=when(error.code) {
            "association_invalid" -> "相似提示响应无效，请刷新后重试"
            "association_stale","association_scope_changed" -> "登录、交易或连接已变化，请刷新后重新排查"
            else -> error.message ?: "相似提示读取失败，请重试"
        }
        state=AssociationHintsState(state.scope,error=message,errorCode=error.code,stale=stale)
        return true
    }

    fun cancel(request:AssociationPageRequest):Boolean {
        if(!current(request))return false
        generation++
        state=AssociationHintsState(state.scope)
        return true
    }

    fun clear() {
        generation++
        state=AssociationHintsState(state.scope)
    }

    private fun current(request:AssociationPageRequest):Boolean =
        state.request==request && request.scope==state.scope && request.generation==generation
}
