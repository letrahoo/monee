package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.ImportUndoPreview

@Composable
internal fun ImportUndoPanel(filename:String,preview:ImportUndoPreview,working:Boolean,onRefresh:()->Unit,onCancel:()->Unit,onConfirm:()->Unit) {
    val restore=preview.action=="restore"
    val blocked=preview.blockedCount>0
    val action=if(restore)"恢复"else"撤销"
    var page by remember(preview){mutableStateOf(0)}
    Surface(modifier=Modifier.fillMaxWidth()) {
        Column(Modifier.padding(22.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
            Text("${action}批次",fontSize=21.sp,color=Pine)
            Text(filename)
            if(blocked) {
                Text("${preview.blockedCount} 笔账单在撤销后发生变化，当前无法恢复整批。",color=MaterialTheme.colors.error)
                Text("账单不会发生变化。请检查下方记录后重新检查。",color=Muted)
            } else {
            Text("将${action} ${preview.changeCount} 笔，保留 ${preview.preservedCount} 笔")
            Text("收入${if(restore)"增加"else"减少"} ${formatMoney(preview.incomeMinor.toLong())} · 支出${if(restore)"增加"else"减少"} ${formatMoney(preview.expenseMinor.toLong())}")
            }
            Text(if(restore)"恢复本次撤销的账单；原始来源与历史保持不变。"else"已修改或关联其他批次的账单会保留。撤销后可从导入记录恢复。",color=Muted,fontSize=13.sp)
            preview.rows.drop(page*10).take(10).forEach { row ->
                Text("${row.date} · ${row.merchant} · ${formatTransactionMoney(row.amountMinor.toLong())}")
                Text("${if(row.action=="preserve")"保留"else action} · ${row.reason}",color=Muted,fontSize=12.sp)
            }
            if(preview.rows.size>10) Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                TextButton(onClick={page--},enabled=page>0&&!working){Text("上一组")}
                TextButton(onClick={page++},enabled=(page+1)*10<preview.rows.size&&!working){Text("下一组")}
            }
            if(preview.changeCount==0)Text("没有可${action}的账单",color=Muted)
            Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                Button(onClick=onConfirm,enabled=!working&&!blocked&&preview.changeCount>0&&!preview.alreadyApplied){Text("确认${action}")}
                TextButton(onClick=onRefresh,enabled=!working){Text("重新检查")}
                TextButton(onClick=onCancel,enabled=!working){Text("返回导入记录")}
            }
        }
    }
}
