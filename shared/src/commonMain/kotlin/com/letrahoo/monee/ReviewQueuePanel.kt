package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.ReviewQueue

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ReviewQueuePanel(queue:ReviewQueue,working:Boolean,onFormat:(String)->Unit,onPage:(Int)->Unit,onClose:()->Unit,onOpenBatch:(String)->Unit) {
    var expanded by remember(queue) { mutableStateOf<String?>(null) }
    Column(verticalArrangement=Arrangement.spacedBy(16.dp)) {
        Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
            Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(6.dp)) {
                Text("待核实",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
                Text("${queue.totalCount} 条来源记录 · 未计入收支",color=Muted,fontSize=13.sp)
            }
            TextButton(onClick=onClose,enabled=!working){Text("返回账单")}
        }
        FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            listOf("" to "全部来源","alipay" to "支付宝","wechat" to "微信").forEach { (value,label) ->
                OutlinedButton(onClick={onFormat(value)},enabled=!working,modifier=Modifier.semantics {selected=queue.format==value},
                    colors=ButtonDefaults.outlinedButtonColors(backgroundColor=if(queue.format==value)Pine else Color.White,contentColor=if(queue.format==value)Color.White else Pine),shape=RoundedCornerShape(20.dp)) {Text(label)}
            }
        }
        if(queue.items.isEmpty()) Surface(color=Color.White,shape=RoundedCornerShape(16.dp),modifier=Modifier.fillMaxWidth()) {
            Text(if(queue.totalCount==0)"当前来源没有待核实记录"else"本页已无记录，请返回上一页",Modifier.padding(24.dp),color=Muted)
        }
        queue.items.forEach { item ->
            Surface(color=Color.White,shape=RoundedCornerShape(16.dp),modifier=Modifier.fillMaxWidth()) {
                Column(Modifier.padding(20.dp),verticalArrangement=Arrangement.spacedBy(10.dp)) {
                    Text(item.merchant.ifBlank {"商户未识别"},color=Ink,fontWeight=FontWeight.SemiBold,fontSize=16.sp)
                    // These fields may be malformed (e.g. 1.001); never round or present as booked money.
                    Text("${item.date.ifBlank {"日期未识别"}} · 原始金额 ${item.amount.ifBlank {"未识别"}}",color=Muted,fontSize=13.sp)
                    Text(item.reason,color=Color(0xFF8A5B16),fontSize=13.sp)
                    val batchLabel=when(item.batchState) {"preview"->"批次未确认";"undone"->"批次已撤销";"failed"->"批次处理失败";else->"批次已确认"}
                    Text("${if(item.format=="wechat")"微信"else"支付宝"} ${item.sourceAccount} · ${item.filename} · 第 ${item.line} 行 · $batchLabel",color=Muted,fontSize=12.sp)
                    FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                        TextButton(onClick={expanded=if(expanded==item.id)null else item.id},enabled=!working){Text(if(expanded==item.id)"收起原始记录"else"查看原始记录")}
                        TextButton(onClick={onOpenBatch(item.importId)},enabled=!working){Text("查看导入批次")}
                    }
                    if(expanded==item.id) {
                        Divider(color=Line)
                        if(item.raw.isEmpty())Text("此记录没有可展示的原始字段",color=Muted,fontSize=12.sp)
                        item.raw.forEach { (name,value)->Text("$name：$value",color=Muted,fontSize=12.sp) }
                    }
                }
            }
        }
        if(queue.totalCount>queue.pageSize||queue.page>1) Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick={onPage(queue.page-1)},enabled=!working&&queue.page>1){Text("上一页")}
            Text("第 ${queue.page} 页",fontSize=12.sp,color=Muted)
            OutlinedButton(onClick={onPage(queue.page+1)},enabled=!working&&queue.page*queue.pageSize<queue.totalCount){Text("下一页")}
        }
    }
}
