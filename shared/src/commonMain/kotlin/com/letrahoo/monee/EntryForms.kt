package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
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
    onCSVChange:(String)->Unit,onPick:()->Unit,onTemplate:(Boolean)->Unit,onPreview:()->Unit,
    onConfirmSimilar:(Boolean)->Unit,onCommit:()->Unit) {
    var previewPage by remember(preview) { mutableStateOf(0) }
    Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
        Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
            Text("导入标准 CSV",fontSize=21.sp,fontWeight=FontWeight.Bold,color=Pine)
            Text("仅支持 UTF-8 标准模板，最多 1000 行 / 2 MiB。支付宝、微信和银行专有格式尚未适配；转账、退款和还款暂不支持。",color=Muted,fontSize=12.sp)
            FlowRow(horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                OutlinedButton(onClick=onPick,enabled=!working){Text("选择 CSV 文件")}
                TextButton(onClick={onTemplate(false)},enabled=!working){Text("填入模板")}
                TextButton(onClick={onTemplate(true)},enabled=!working){Text("填入合成示例")}
            }
            Text("文件：$filename",fontSize=12.sp,color=Muted)
            OutlinedTextField(csv,onValueChange=onCSVChange,modifier=Modifier.fillMaxWidth().heightIn(min=150.dp,max=240.dp),enabled=!working,
                label={Text("CSV 内容，可直接粘贴")},textStyle=LocalTextStyle.current.copy(fontSize=12.sp))
            Text("date 日期、type 收支、amount 正数金额（元）、currency 固定 CNY、merchant 商户、source 来源为必填列。category 分类、account 资金账户、external_id 来源流水号、note 备注可选。",fontSize=12.sp,color=Muted)
            Button(onClick=onPreview,enabled=csv.isNotBlank()&&!working){Text("预览账单")}
            preview?.let { p ->
                if(p.alreadyCommitted) Text("此文件已入账，重复导入不会新增账单。",color=Pine)
                else Text("将新增 ${p.newCount} 笔 · 跳过 ${p.duplicateCount} 笔重复流水 · ${p.similarCount} 笔待核实",fontWeight=FontWeight.Bold)
                p.errors.forEach { Text(it,color=MaterialTheme.colors.error,fontSize=13.sp) }
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
                    Text("已核实以上相似记录是独立交易，需要继续入账。",fontSize=13.sp)
                }
                if(!p.alreadyCommitted) Button(onClick=onCommit,enabled=p.id.isNotEmpty()&&p.errors.isEmpty()&&(!working)&&(p.similarCount==0||confirmedSimilar)){
                    Text("确认入账 ${p.newCount} 笔")
                }
                Text("确认后保存整个文件内通过校验的记录。示例同样会保存，请勿将示例当成真实账单。",fontSize=12.sp,color=Muted)
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
            OutlinedTextField(input.amount,{onChange(input.copy(amount=it))},Modifier.fillMaxWidth(),label={Text("金额（元，正数）")},singleLine=true,enabled=!working,keyboardOptions=KeyboardOptions(keyboardType=KeyboardType.Decimal))
            OutlinedTextField(input.merchant,{onChange(input.copy(merchant=it))},Modifier.fillMaxWidth(),label={Text("商户或说明")},singleLine=true,enabled=!working)
            OutlinedTextField(input.category,{onChange(input.copy(category=it))},Modifier.fillMaxWidth(),label={Text("分类（留空为未分类）")},singleLine=true,enabled=!working)
            OutlinedTextField(input.source,{onChange(input.copy(source=it))},Modifier.fillMaxWidth(),label={Text("账单来源")},singleLine=true,enabled=!working)
            OutlinedTextField(input.account,{onChange(input.copy(account=it))},Modifier.fillMaxWidth(),label={Text("资金账户（留空为待核实）")},singleLine=true,enabled=!working)
            OutlinedTextField(input.note,{onChange(input.copy(note=it))},Modifier.fillMaxWidth(),label={Text("备注")},singleLine=true,enabled=!working)
            Text("当前仅支持普通收入和支出，请勿用来记录转账、退款或信用账户还款。",fontSize=12.sp,color=Muted)
            Button(onClick=onSave,enabled=!working&&input.amount.isNotBlank()&&input.merchant.isNotBlank()){Text("保存账单")}
        }
    }
}
