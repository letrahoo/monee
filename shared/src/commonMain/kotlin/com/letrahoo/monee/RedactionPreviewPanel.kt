package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.RedactionPreview

@Composable
internal fun RedactionPreviewPanel(preview:RedactionPreview,onClose:()->Unit) {
    var page by remember(preview) { mutableStateOf(0) }
    var expanded by remember(preview) { mutableStateOf<String?>(null) }
    var excludedPage by remember(preview) { mutableStateOf(0) }
    Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
        Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
            Text("脱敏预览",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
            TextButton(onClick=onClose) { Text("收起脱敏预览") }
            if(!preview.localOnly) {
                Text("预览状态异常，请重新获取。",color=MaterialTheme.colors.error)
                return@Column
            }
            Text("仅在本机生成，尚未发送。",color=Pine)
            Text("按原始账单生成，不含后续账单修改。",fontSize=13.sp,color=Muted)
            Text("日期和金额保留；账户、商户与订单号替换为关联标识；商品、备注等自由文字不包含在预览中。",fontSize=13.sp,color=Muted)
            Text("可用 ${preview.includedCount} 条 · 排除 ${preview.excluded.size} 条",fontWeight=FontWeight.SemiBold)
            if(preview.includedCount==0) Text("没有可用于分析的有效记录。",color=Muted)
            preview.payload.records.drop(page*10).take(10).forEach { record ->
                Divider()
                val direction=when(record.direction) {"expense"->"支出";"income"->"收入";else->"性质待核实"}
                Text("${record.date} · ${formatMoney(record.amountMinor.toLong())} · $direction",fontSize=14.sp)
                TextButton(onClick={expanded=if(expanded==record.ref)null else record.ref}) {
                    Text(if(expanded==record.ref)"收起脱敏字段"else"查看脱敏字段")
                }
                if(expanded==record.ref) SelectionContainer {
                    Column(verticalArrangement=Arrangement.spacedBy(6.dp)) {
                        Text("记录标识：${record.ref}",fontSize=12.sp,color=Muted)
                        Text("日期：${record.date}\n金额（分）：${record.amountMinor}\n币种：${record.currency}\n收支：${record.direction}\n来源：${record.source}",fontSize=12.sp,color=Muted)
                        if(record.accountRef.isNotEmpty()) Text("账户标识：${record.accountRef}",fontSize=12.sp,color=Muted)
                        if(record.merchantRef.isNotEmpty()) Text("商户标识：${record.merchantRef}",fontSize=12.sp,color=Muted)
                        record.identifiers.forEach { id -> Text("${id.kind}：${id.ref}",fontSize=12.sp,color=Muted) }
                    }
                }
            }
            if(preview.payload.records.size>10) Row {
                TextButton(onClick={page--;expanded=null},enabled=page>0){Text("上一组记录")}
                TextButton(onClick={page++;expanded=null},enabled=(page+1)*10<preview.payload.records.size){Text("下一组记录")}
            }
            if(preview.excluded.isNotEmpty()) {
                Text("未包含的记录",fontWeight=FontWeight.SemiBold)
                preview.excluded.drop(excludedPage*10).take(10).forEach { row ->
                    Text("第 ${row.line} 行 · ${row.reason}",fontSize=13.sp,color=Muted)
                }
                if(preview.excluded.size>10) Row {
                    TextButton(onClick={excludedPage--},enabled=excludedPage>0){Text("上一组排除项")}
                    TextButton(onClick={excludedPage++},enabled=(excludedPage+1)*10<preview.excluded.size){Text("下一组排除项")}
                }
            }
        }
    }
}
