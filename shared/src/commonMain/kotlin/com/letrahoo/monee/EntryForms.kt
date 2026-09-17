package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.*

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ImportPanel(csv:String,filename:String,preview:ImportPreview?,confirmedSimilar:Boolean,working:Boolean,
    format:String,hasNativeFile:Boolean,sourceAccount:String,onMode:(String)->Unit,onAccount:(String)->Unit,
    onCSVChange:(String)->Unit,onPick:()->Unit,onTemplate:()->Unit,onPreview:()->Unit,
    onConfirmSimilar:(Boolean)->Unit,onCommit:()->Unit) {
    val native = format != "standard"
    val wechat = format == "wechat"
    var previewPage by remember(preview) { mutableStateOf(0) }
    var showFormat by remember { mutableStateOf(false) }
    var evidenceLine by remember(preview) { mutableStateOf<Int?>(null) }
    Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
        Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
            Text("导入账单",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
            ImportSourceSelector(format,working,onMode)
            Text(if(wechat)"选择微信账单 CSV 或 XLSX。最多 1000 条、2 MB。"else if(native)"直接选择支付宝导出的 CSV。最多 1000 条、2 MB。"else"按模板整理人民币收入和支出，最多 1000 行、2 MB。",color=Muted,fontSize=12.sp)
            if(native) {
                OutlinedTextField(sourceAccount,onAccount,Modifier.widthIn(max=460.dp).fillMaxWidth(),label={Text("来源账户名称")},singleLine=true,enabled=!working)
                Text("同一来源账户请保持名称一致；多个账户使用不同名称。",fontSize=12.sp,color=Muted)
            }
            if(!native) TextButton(onClick=onTemplate,enabled=!working){Text("使用模板")}
            Surface(color=Color(0xFFF3F6F2),shape=RoundedCornerShape(12.dp),modifier=Modifier.fillMaxWidth()) {
                Row(Modifier.padding(16.dp),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                    Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(4.dp)) {
                        Text(if((native&&hasNativeFile)||csv.isNotBlank()||preview!=null)filename else "尚未选择文件",color=Ink,fontSize=14.sp,fontWeight=FontWeight.Medium)
                        Text(if(wechat)"微信 CSV / XLSX"else if(native)"支付宝导出的 CSV" else "按模板整理的 CSV",color=Muted,fontSize=12.sp)
                    }
                    OutlinedButton(onClick=onPick,enabled=!working){Text(if(hasNativeFile||csv.isNotBlank())"更换文件" else "选择文件")}
                }
            }
            if(!native) OutlinedTextField(csv,onValueChange=onCSVChange,modifier=Modifier.fillMaxWidth().heightIn(min=150.dp,max=240.dp),enabled=!working,
                label={Text("CSV 内容，可直接粘贴")},textStyle=LocalTextStyle.current.copy(fontSize=12.sp))
            TextButton(onClick={showFormat=!showFormat}){Text(if(showFormat)"收起格式说明"else"格式说明")}
            if(showFormat&&native) Text((if(wechat)"CSV 需为 UTF-8 且包含微信账单表头；XLSX 仅支持单工作表。请勿改写订单号。"else"支持 UTF-8 和 GB18030。")+"退款、转账及不能确定的记录会保留为待核实，不计入收支；确认后仅保存通过校验的记录。",fontSize=12.sp,color=Muted)
            if(showFormat&&!native) Text("UTF-8 编码。必填：date 日期、type 收支、amount 金额（元）、currency 币种（CNY）、merchant 商户、source 来源。可选：category 分类、account 账户、external_id 流水号、note 备注。转账、退款和还款暂不支持。",fontSize=12.sp,color=Muted)
            Button(onClick=onPreview,enabled=(if(native)hasNativeFile&&sourceAccount.isNotBlank()else csv.isNotBlank())&&!working){Text("预览账单")}
            preview?.let { p ->
                if(p.alreadyCommitted) Text(if(p.undone)"此批次已撤销，重复导入不会重新入账。可在导入记录中恢复。"else"此文件已入账，重复导入不会新增账单。",color=Pine)
                else Text("将新增 ${p.newCount} 笔 · 跳过 ${p.duplicateCount} 笔重复流水 · ${p.similarCount} 笔疑似重复",fontWeight=FontWeight.Bold)
                if(p.pending.isNotEmpty()) {
                    if(p.id.isNotEmpty()&&p.errors.isEmpty()) Text("待核实记录已保留，可从导入记录重新查看。",fontSize=12.sp,color=Muted)
                    Text("${p.pending.size} 笔待核实 · 未计入收支",color=Color(0xFF8A5B16),fontWeight=FontWeight.Bold)
                    var pendingPage by remember(p) { mutableStateOf(0) }
                    p.pending.drop(pendingPage*10).take(10).forEach { row ->
                        Text("第 ${row.line} 行 · ${row.date} · ${row.merchant} · ${row.amount} 元\n${row.reason}",fontSize=12.sp,color=Muted)
                    }
                    if(p.pending.size>10) Row {
                        TextButton(onClick={pendingPage--},enabled=pendingPage>0){Text("上一组待核实")}
                        TextButton(onClick={pendingPage++},enabled=(pendingPage+1)*10<p.pending.size){Text("下一组待核实")}
                    }
                }
                p.errors.forEach { Text(it,color=MaterialTheme.colors.error,fontSize=13.sp) }
                if(p.errors.isNotEmpty()&&p.id.isNotEmpty())Text("失败记录已保存，可从导入记录查看。",fontSize=12.sp,color=Muted)
                p.sourceDocument?.let { source ->
                    var showEvidence by remember(p) { mutableStateOf(false) }
                    var evidencePage by remember(p) { mutableStateOf(0) }
                    TextButton(onClick={showEvidence=!showEvidence}) { Text(if(showEvidence)"收起来源明细"else"查看来源明细（${source.records.size} 条）") }
                    if(showEvidence) {
                        source.records.drop(evidencePage*10).take(10).forEach { original ->
                            TextButton(onClick={evidenceLine=if(evidenceLine==original.line)null else original.line}) { Text("原文件第 ${original.line} 行") }
                            if(evidenceLine==original.line) original.raw.forEach { (name,value) -> Text("$name：$value",fontSize=12.sp,color=Muted) }
                        }
                        if(source.records.size>10) Row {
                            TextButton(onClick={evidencePage--;evidenceLine=null},enabled=evidencePage>0){Text("上一组来源")}
                            TextButton(onClick={evidencePage++;evidenceLine=null},enabled=(evidencePage+1)*10<source.records.size){Text("下一组来源")}
                        }
                    }
                }
                p.rows.drop(previewPage*20).take(20).forEach { row ->
                    Column(verticalArrangement=Arrangement.spacedBy(4.dp)) {
                        Row(Modifier.fillMaxWidth()) {
                            Text("${row.line} · ${row.record.date} ${row.record.merchant}",Modifier.weight(1f),fontSize=13.sp)
                            Text(formatMoney(row.record.amountMinor.toLong()),fontSize=13.sp)
                        }
                        Text("${row.record.category} · ${row.record.source}${if(row.message.isNotEmpty())" · ${row.message}"else""}",fontSize=12.sp,
                            color=if(row.status=="similar"||row.status=="conflict")MaterialTheme.colors.error else Muted)
                    }
                }
                if(p.rows.size>20) Row(horizontalArrangement=Arrangement.spacedBy(8.dp),verticalAlignment=Alignment.CenterVertically) {
                    TextButton(onClick={previewPage--},enabled=previewPage>0){Text("上 20 行")}
                    Text("共 ${p.rows.size} 行",fontSize=12.sp)
                    TextButton(onClick={previewPage++},enabled=(previewPage+1)*20<p.rows.size){Text("下 20 行")}
                }
                if(p.similarCount>0&&!p.alreadyCommitted) Row(verticalAlignment=Alignment.CenterVertically) {
                    Checkbox(confirmedSimilar,onCheckedChange=onConfirmSimilar,enabled=!working)
                    Text("这些相似账单是不同交易，继续入账。",fontSize=13.sp)
                }
                if(!p.alreadyCommitted) Button(onClick=onCommit,enabled=p.id.isNotEmpty()&&p.rows.isNotEmpty()&&p.errors.isEmpty()&&(!working)&&(p.similarCount==0||confirmedSimilar)){
                    Text("确认入账 ${p.newCount} 笔")
                }
            }
        }
    }
}

@Composable
internal fun ManualPanel(input:TransactionInput,working:Boolean,onChange:(TransactionInput)->Unit,onSave:()->Unit) {
    Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
        Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
            Text("记一笔",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
            Row(horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                if(input.type=="expense")Button(onClick={onChange(input.copy(type="expense"))},enabled=!working){Text("支出")}
                else OutlinedButton(onClick={onChange(input.copy(type="expense"))},enabled=!working){Text("支出")}
                if(input.type=="income")Button(onClick={onChange(input.copy(type="income"))},enabled=!working){Text("收入")}
                else OutlinedButton(onClick={onChange(input.copy(type="income"))},enabled=!working){Text("收入")}
            }
            OutlinedTextField(input.date,{onChange(input.copy(date=it))},Modifier.fillMaxWidth(),label={Text("日期 YYYY-MM-DD")},singleLine=true,enabled=!working)
            OutlinedTextField(input.amount,{onChange(input.copy(amount=it))},Modifier.fillMaxWidth(),label={Text("金额（元）")},singleLine=true,enabled=!working,keyboardOptions=KeyboardOptions(keyboardType=KeyboardType.Decimal))
            OutlinedTextField(input.merchant,{onChange(input.copy(merchant=it))},Modifier.fillMaxWidth(),label={Text("商户或说明")},singleLine=true,enabled=!working)
            OutlinedTextField(input.category,{onChange(input.copy(category=it))},Modifier.fillMaxWidth(),label={Text("分类（留空为未分类）")},singleLine=true,enabled=!working)
            OutlinedTextField(input.source,{onChange(input.copy(source=it))},Modifier.fillMaxWidth(),label={Text("账单来源")},singleLine=true,enabled=!working)
            OutlinedTextField(input.account,{onChange(input.copy(account=it))},Modifier.fillMaxWidth(),label={Text("资金账户（留空为待核实）")},singleLine=true,enabled=!working)
            OutlinedTextField(input.note,{onChange(input.copy(note=it))},Modifier.fillMaxWidth(),label={Text("备注")},singleLine=true,enabled=!working)
            Text("暂不支持转账、退款和还款。",fontSize=12.sp,color=Muted)
            Button(onClick=onSave,enabled=!working&&input.amount.isNotBlank()&&input.merchant.isNotBlank()){Text("保存账单")}
        }
    }
}

@Composable
internal fun ImportHistoryPanel(history:ImportHistory,working:Boolean,onPage:(Int)->Unit,onOpen:(String)->Unit,canManage:Boolean,onManage:(ImportSummary)->Unit) {
    Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
        Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
            Text("导入记录",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
            if(history.imports.isEmpty()) Text("暂无导入记录",color=Muted)
            history.imports.forEach { item ->
                OutlinedButton(onClick={onOpen(item.id)},enabled=!working,modifier=Modifier.fillMaxWidth()) {
                    Column(Modifier.fillMaxWidth()) {
                        Text(item.filename)
                        val result=item.result
                        Text(if(item.undone)"已撤销 · 可恢复 · 原批次新增 ${result?.added ?: 0} 笔"else if(item.errors>0)"解析失败 · ${item.errors} 项错误"else if(result==null)"未入账 · 待核实 ${item.pending} 笔 · ${item.createdAt.take(10)}"else"已入账 ${result.added} 笔 · 重复 ${result.skipped} 笔 · 待核实 ${result.pending} 笔",fontSize=12.sp,color=Muted)
                    }
                }
                if(canManage&&item.committedAt!=null) TextButton(onClick={onManage(item)},enabled=!working){Text(if(item.undone)"恢复此批次"else"撤销此批次")}
            }
            Row(verticalAlignment=Alignment.CenterVertically) {
                TextButton(onClick={onPage(history.page-1)},enabled=!working&&history.page>1){Text("上一页")}
                Text("第 ${history.page} 页 · 共 ${history.totalCount} 次",fontSize=12.sp)
                TextButton(onClick={onPage(history.page+1)},enabled=!working&&history.page*history.pageSize<history.totalCount){Text("下一页")}
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ImportSourceSelector(format:String,disabled:Boolean,onMode:(String)->Unit) {
    Surface(color=Color(0xFFEEF2ED),shape=RoundedCornerShape(12.dp)) {
        FlowRow(Modifier.padding(4.dp),horizontalArrangement=Arrangement.spacedBy(4.dp)) {
            listOf("alipay" to "支付宝", "wechat" to "微信", "standard" to "标准 CSV").forEach { (value,label) ->
                TextButton(onClick={onMode(value)},enabled=!disabled,modifier=Modifier.semantics { selected = format==value },
                    shape=RoundedCornerShape(9.dp),
                    colors=ButtonDefaults.textButtonColors(backgroundColor=if(format==value)Color.White else Color.Transparent,contentColor=if(format==value)Pine else Muted),
                    contentPadding=PaddingValues(horizontal=20.dp,vertical=12.dp)) {
                    Text(label,fontWeight=if(format==value)FontWeight.SemiBold else FontWeight.Normal)
                }
            }
        }
    }
}

@Composable
internal fun AnnotationEditor(item:LedgerTransaction,working:Boolean,onSave:(String,String)->Unit) {
    var editing by remember(item.id,item.version){mutableStateOf(false)}
    var category by remember(item.id,item.version){mutableStateOf(item.category)}
    var note by remember(item.id,item.version){mutableStateOf(item.note)}
    if(!editing) TextButton(onClick={editing=true},enabled=!working){Text("修改分类与备注")}
    else Column(verticalArrangement=Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(category,{category=it},label={Text("分类")},enabled=!working,singleLine=true,modifier=Modifier.fillMaxWidth())
        OutlinedTextField(note,{note=it},label={Text("备注")},enabled=!working,modifier=Modifier.fillMaxWidth())
        Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
            Button(onClick={onSave(category,note)},enabled=!working&&(category!=item.category||note!=item.note)){Text("保存修改")}
            TextButton(onClick={editing=false;category=item.category;note=item.note},enabled=!working){Text("取消")}
        }
    }
}
