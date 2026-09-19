package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.letrahoo.monee.data.*

internal fun transferAccountsValid(from:String,to:String):Boolean =
    from.trim().isNotEmpty()&&to.trim().isNotEmpty()&&from.trim()!=to.trim()&&
        from.trim()!="待核实资金账户"&&to.trim()!="待核实资金账户"

internal fun transactionAmountLabel(item:LedgerTransaction):String =
    if(item.type=="transfer")"转账 ${formatMoney(item.amountMinor.toLong())}"
    else formatTransactionMoney(item.amountMinor.toLong())

internal fun transactionAccountLabel(item:LedgerTransaction):String =
    if(item.type=="transfer")"${item.account} → ${item.toAccount}"
    else "资金账户：${item.account}（待核实）"

@Composable
internal fun TransferPanel(today:String,readOnly:Boolean,working:Boolean,pending:TransferSubmission?,
    onSubmit:(TransferInput)->Unit,onClose:()->Unit) {
    var date by remember { mutableStateOf(today) }
    var amount by remember { mutableStateOf("") }
    var from by remember { mutableStateOf("") }
    var to by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    var confirmed by remember { mutableStateOf(false) }
    val frozen=pending?.input
    val locked=readOnly||working||pending!=null
    Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        Text("本人账户间转账",color=Pine)
        Text("仅登记已发生的资金转移，不实际转款，不计收入或支出。信用卡/白条还款不在此登记。",color=Muted)
        OutlinedTextField(frozen?.date ?: date,{date=it},label={Text("转账日期（YYYY-MM-DD）")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
        OutlinedTextField(frozen?.amount ?: amount,{amount=it},label={Text("转账金额（元）")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
        OutlinedTextField(frozen?.fromAccount ?: from,{from=it;confirmed=false},label={Text("转出账户")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
        OutlinedTextField(frozen?.toAccount ?: to,{to=it;confirmed=false},label={Text("转入账户")},singleLine=true,enabled=!locked,modifier=Modifier.fillMaxWidth())
        Text("两侧账户必须明确且不同；缺失的一侧应留待核实，不猜测补齐。",color=Muted)
        OutlinedTextField(frozen?.note ?: note,{note=it},label={Text("备注")},enabled=!locked,modifier=Modifier.fillMaxWidth())
        Row {
            Checkbox(checked=pending!=null||confirmed,onCheckedChange={confirmed=it},enabled=!locked)
            Text("确认两侧均为本人资金账户",modifier=Modifier.padding(top=12.dp))
        }
        if(pending!=null)Text("上次保存结果尚未确认。重试使用原请求，不会重复记账。",color=Muted)
        if(readOnly)Text("当前仅可查看，不能登记或重试转账。",color=Muted)
        Button(onClick={onSubmit(frozen ?: TransferInput(date.trim(),amount.trim(),from.trim(),to.trim(),note=note.trim()))},
            enabled=!readOnly&&!working&&(pending!=null||confirmed&&date.isNotBlank()&&correctionMinorInput(amount.trim())!=null&&transferAccountsValid(from,to))) {
            Text(if(pending!=null)"重试确认转账"else"保存转账")
        }
        TextButton(onClick=onClose,enabled=!working){Text("返回账本")}
    }
}
