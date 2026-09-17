package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.letrahoo.monee.data.*

// Input conversion only; the service validates limits and calculates all ledger impacts.
internal fun correctionMinorInput(text:String):String? {
    if(!Regex("[0-9]+(?:\\.[0-9]{1,2})?").matches(text))return null
    val parts=text.split('.')
    val digits=(parts[0]+parts.getOrElse(1){""}.padEnd(2,'0')).trimStart('0').ifEmpty{"0"}
    return digits.takeIf { (it.toLongOrNull() ?: 0L)>0L }
}

@Composable
internal fun CorrectionEditor(item:LedgerTransaction,working:Boolean,
    onPreview:(CorrectionInput,(CorrectionPreview)->Unit)->Unit,onConfirm:(CorrectionInput)->Unit) {
    var editing by remember(item.id,item.version){mutableStateOf(false)}
    var kind by remember(item.id,item.version){mutableStateOf(item.type)}
    val original=item.amountMinor.removePrefix("-").padStart(3,'0')
    var amount by remember(item.id,item.version){mutableStateOf(original.dropLast(2)+"."+original.takeLast(2))}
    var reason by remember(item.id,item.version){mutableStateOf("")}
    var checked by remember(item.id,item.version){mutableStateOf<Pair<CorrectionInput,CorrectionPreview>?>(null)}
    if(!editing) {
        TextButton(onClick={editing=true},enabled=!working){Text("更正金额与收支")}
        return
    }
    Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(8.dp)) {
        Text("更正账单")
        Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            listOf("expense" to "支出","income" to "收入").forEach { (value,label)->
                OutlinedButton(onClick={kind=value;checked=null},enabled=!working&&kind!=value){Text(if(kind==value)"✓ $label"else label)}
            }
        }
        val minor=correctionMinorInput(amount)
        OutlinedTextField(amount,{amount=it;checked=null},label={Text("金额（元）")},singleLine=true,enabled=!working,isError=amount.isNotEmpty()&&minor==null,modifier=Modifier.fillMaxWidth())
        if(amount.isNotEmpty()&&minor==null)Text("请输入正数金额，最多两位小数",color=MaterialTheme.colors.error)
        OutlinedTextField(reason,{reason=it;checked=null},label={Text("更正原因")},enabled=!working,isError=reason.length>500,modifier=Modifier.fillMaxWidth())
        val change=minor?.let { CorrectionInput(item.version,kind,it,reason.trim()) }
        val changed=kind!=item.type||minor!=item.amountMinor.removePrefix("-")
        if(checked==null) Button(onClick={change?.let { input->onPreview(input){ result->checked=input to result } }},enabled=!working&&change!=null&&changed&&reason.isNotBlank()&&reason.length<=500){Text("查看更正影响")}
        checked?.let { (input,impact)->
            Text("${if(impact.before.type=="income")"收入"else"支出"} ${formatMoney(impact.before.amountMinor.toLong())} → ${if(impact.after.type=="income")"收入"else"支出"} ${formatMoney(impact.after.amountMinor.toLong())}")
            Text("本月收入变化：${formatTransactionMoney(impact.incomeDeltaMinor.toLong())}\n本月支出变化：${formatTransactionMoney(impact.expenseDeltaMinor.toLong())}\n本月结余变化：${formatTransactionMoney(impact.netDeltaMinor.toLong())}")
            Text("原始账单保持不变，更正原因会保留在修改记录中。",color=Muted)
            Button(onClick={onConfirm(input)},enabled=!working){Text("确认更正")}
        }
        TextButton(onClick={editing=false;checked=null},enabled=!working){Text("取消更正")}
    }
}
