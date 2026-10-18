package com.letrahoo.monee.data

import kotlinx.serialization.Serializable

// Required fields intentionally have no defaults: missing read-only declarations
// or versions are malformed responses, not permission to display a safe guess.
@Serializable
data class AssociationTransaction(
    val id:String,
    val version:Long,
    val date:String,
    val type:String,
    val amountMinor:String,
    val currency:String,
    val merchant:String,
    val sourceDisplay:String,
)

@Serializable
data class AssociationCandidate(
    val transaction:AssociationTransaction,
    val reasons:List<String>,
)

@Serializable
data class AssociationCandidates(
    val contractVersion:Int,
    val readOnly:Boolean,
    val basis:String,
    val sourceRolesVerified:Boolean,
    val actions:List<String>,
    val ledgerVersion:Long,
    val base:AssociationTransaction,
    val items:List<AssociationCandidate>,
    val page:Int,
    val pageSize:Int,
    val totalCount:Int,
)

internal enum class AssociationValidation { Valid, Stale, Invalid }

internal fun validateAssociationCandidates(
    data:AssociationCandidates,
    scope:AssociationQueryScope,
    requestedPage:Int,
):AssociationValidation {
    if(!validAssociationEnvelope(data,scope.transactionId,requestedPage))return AssociationValidation.Invalid
    if(scope.userId.isBlank() || scope.ledgerId.isBlank() || scope.transactionVersion<1 || scope.ledgerVersion<0)return AssociationValidation.Invalid
    if(data.ledgerVersion!=scope.ledgerVersion || data.base.version!=scope.transactionVersion)return AssociationValidation.Stale
    return AssociationValidation.Valid
}

// The transport does not own UI ledger/base versions. It checks the complete
// envelope here; the controller separately compares the immutable query scope.
internal fun validAssociationEnvelope(data:AssociationCandidates,id:String,page:Int):Boolean {
    if(data.contractVersion!=1 || !data.readOnly || data.basis!="current_transaction_similarity" || data.sourceRolesVerified || data.actions.isNotEmpty())return false
    if(page !in 1..1_000_000 || data.page!=page || data.pageSize!=50 || data.ledgerVersion<0 || data.totalCount<0 || data.base.id!=id || !validAssociationTransaction(data.base))return false
    val offset=(page-1L)*50L
    val expected=(data.totalCount.toLong()-offset).coerceIn(0L,50L).toInt()
    if(data.items.size!=expected)return false
    val seen=mutableSetOf<String>()
    var previous:String?=null
    for(item in data.items) {
        val next=item.transaction
        if(item.reasons!=listOf("current_projection_match","different_source_display") || !validAssociationTransaction(next))return false
        if(next.id==data.base.id || !seen.add(next.id) || previous?.let { it>=next.id }==true)return false
        if(next.date!=data.base.date || next.type!=data.base.type || next.amountMinor!=data.base.amountMinor || next.currency!=data.base.currency || next.merchant!=data.base.merchant || next.sourceDisplay==data.base.sourceDisplay)return false
        previous=next.id
    }
    return true
}

private fun validAssociationTransaction(value:AssociationTransaction):Boolean {
    if(value.id.isBlank() || value.id.any { it.category==CharCategory.CONTROL } || value.version<1 || value.type!="expense" || value.currency!="CNY")return false
    val minor=value.amountMinor.toLongOrNull() ?: return false
    if(minor !in -1_000_000_000_000L..-1L || minor.toString()!=value.amountMinor)return false
    return validAssociationDate(value.date) && validAssociationDisplay(value.merchant) && validAssociationDisplay(value.sourceDisplay)
}

private fun validAssociationDate(value:String):Boolean {
    if(value.length!=10 || value[4]!='-' || value[7]!='-' || value.filterIndexed { index, _ -> index!=4 && index!=7 }.any { it !in '0'..'9' })return false
    val year=value.substring(0,4).toIntOrNull() ?: return false
    val month=value.substring(5,7).toIntOrNull() ?: return false
    val day=value.substring(8,10).toIntOrNull() ?: return false
    if(year !in 1900..2200 || month !in 1..12)return false
    val leap=year%4==0 && (year%100!=0 || year%400==0)
    val days=when(month) { 2->if(leap)29 else 28;4,6,9,11->30;else->31 }
    return day in 1..days
}

// Count Unicode scalar values rather than UTF-16 code units, matching Go's rune
// limits. Unpaired surrogates and control characters are never display evidence.
private fun validAssociationDisplay(value:String):Boolean {
    if(value.isEmpty() || value.trim()!=value)return false
    var index=0
    var count=0
    while(index<value.length) {
        val char=value[index]
        if(char.category==CharCategory.CONTROL)return false
        when(char.code) {
            in 0xD800..0xDBFF -> {
                if(index+1>=value.length || value[index+1].code !in 0xDC00..0xDFFF)return false
                index++
            }
            in 0xDC00..0xDFFF -> return false
        }
        count++
        if(count>200)return false
        index++
    }
    return true
}
