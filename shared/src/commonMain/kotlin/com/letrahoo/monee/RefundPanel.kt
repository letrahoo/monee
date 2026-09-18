package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.letrahoo.monee.data.*

// Chart presentation only; all accounting amounts come from the Go service.
internal fun expenseShare(gross:String,total:String):Float {
    val denominator=total.toLongOrNull() ?: return 0f
    if(denominator<=0)return 0f
    return ((gross.toLongOrNull() ?: 0L).toDouble()/denominator.toDouble()).toFloat().coerceIn(0f,1f)
}

@Composable
internal fun RefundPanel(summary:RefundSummary,today:String,readOnly:Boolean,working:Boolean,
    pending:RefundSubmission?,onSubmit:(RefundInput)->Unit,onRefresh:()->Unit,onClose:()->Unit) {
    val original=summary.original
    var date by remember(original.id){mutableStateOf(today)}
    var amount by remember(original.id){mutableStateOf("")}
    var account by remember(original.id){mutableStateOf(original.account)}
    var note by remember(original.id){mutableStateOf("")}
    val frozen=pending?.input
    val locked=working||pending!=null
    Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        Text("退款 · ${original.merchant}",color=Pine)
        Text("原消费：${original.date} · ${formatMoney(original.amountMinor.toLong().let { -it })}")
        Text("已退款：${formatMoney(summary.refundedMinor.toLong())} · 剩余可退：${formatMoney(summary.remainingMinor.toLong())}")
        Text("退款按实际退款日期抵减当月支出，不记为收入，也不改写过去月份。",color=Muted)
        if(summary.refunds.isEmpty())Text("暂无退款记录",color=Muted)
        summary.refunds.forEach { refund ->
            Text("${refund.date} · 退款 ${formatMoney(refund.amountMinor.toLong())} · ${refund.account}")
            if(refund.note.isNotBlank())Text(refund.note,color=Muted)
        }
        TextButton(onClick=onRefresh,enabled=!working){Text("刷新退款记录")}
        if(!readOnly&&(summary.remainingMinor.toLong()>0||pending!=null)) {
            Divider()
            OutlinedTextField(frozen?.date ?: date,{date=it},label={Text("退款日期（YYYY-MM-DD）")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
            OutlinedTextField(frozen?.amount ?: amount,{amount=it},label={Text("退款金额（元）")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
            OutlinedTextField(frozen?.account ?: account,{account=it},label={Text("退回账户")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
            OutlinedTextField(frozen?.note ?: note,{note=it},label={Text("退款备注")},enabled=!locked,modifier=Modifier.fillMaxWidth())
            if(pending!=null)Text("上次保存结果尚未确认。重试使用同一个请求，不会重复记账。",color=Muted)
            Button(onClick={onSubmit(frozen ?: RefundInput(original.version,date.trim(),amount.trim(),original.currency,account.trim(),note.trim()))},enabled=!working&&(pending!=null||date.isNotBlank()&&correctionMinorInput(amount.trim())!=null)) {
                Text(if(pending!=null)"重试确认退款"else"保存退款")
            }
        }
        TextButton(onClick=onClose,enabled=!working){Text("返回账本")}
    }
}
