package com.letrahoo.monee

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.*
import com.letrahoo.monee.resources.Res
import com.letrahoo.monee.resources.logo
import com.letrahoo.monee.resources.noto_sans_sc
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.Font
import org.jetbrains.compose.resources.painterResource
import kotlin.random.Random

internal val Pine = Color(0xFF165B4A)
internal val Ink = Color(0xFF20372F)
internal val Muted = Color(0xFF6E7F76)
private val Paper = Color(0xFFF6F7F2)
private val Line = Color(0xFFE4E9E1)

private fun requestKey() = List(32) { "0123456789abcdef"[Random.nextInt(16)] }.joinToString("")

@Composable
internal fun LedgerScreen(api:LedgerApi,user:AuthUser,ledger:LedgerInfo,onWorkspace:()->Unit,authBusy:Boolean,authError:String?,onLogout:()->Unit,onManage:()->Unit,onAccessLost:()->Unit) {
    val scope = rememberCoroutineScope()
    var dashboard by remember { mutableStateOf<Dashboard?>(null) }
    var month by remember { mutableStateOf("") }
    var query by remember { mutableStateOf("") }
    var page by remember { mutableStateOf(1) }
    var refresh by remember { mutableStateOf(0) }
    var loading by remember { mutableStateOf(true) }
    var working by remember { mutableStateOf(false) }
    var connectionError by remember { mutableStateOf<String?>(null) }
    var actionError by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var panel by remember { mutableStateOf("") }
    var selectedID by remember { mutableStateOf<String?>(null) }
    var csv by remember { mutableStateOf("") }
    var filename by remember { mutableStateOf("import.csv") }
    var preview by remember { mutableStateOf<ImportPreview?>(null) }
    var confirmedSimilar by remember { mutableStateOf(false) }
    var input by remember { mutableStateOf(TransactionInput()) }
    var createKey by remember { mutableStateOf(requestKey()) }

    LaunchedEffect(month, query, page, refresh) {
        loading = true
        try {
            if (query.isNotEmpty()) delay(250)
            dashboard = api.dashboard(ledger.id,month,query,page)
            connectionError = null
        } catch (e: CancellationException) { throw e
        } catch (e: Exception) { if(e is LedgerException&&e.accessLost)onAccessLost() else {dashboard=null;connectionError = e.message ?: "本地服务暂不可用"}
        } finally { loading = false }
    }
    LaunchedEffect(Unit) {
        while (true) { delay(5000); if (!working && !loading) refresh++ }
    }
    fun runAction(action: suspend () -> Unit) {
        scope.launch {
            working = true; actionError = null; notice = null
            try { action() } catch (e: CancellationException) { throw e
            } catch (e: Exception) { if(e is LedgerException&&e.accessLost)onAccessLost() else actionError = e.message ?: "操作失败，请检查后重试"
            } finally { working = false }
        }
    }
    fun changeCSV(value: String) { csv = value; preview = null; confirmedSimilar = false }
    fun saved(savedMonth: String, message: String) {
        month = savedMonth; query = ""; page = 1; panel = ""; selectedID = null
        notice = message; refresh++
    }

    MaterialTheme(
        colors = lightColors(primary=Pine,secondary=Color(0xFFE4B940),background=Paper,surface=Color.White,onBackground=Ink,onSurface=Ink),
        typography = Typography(defaultFontFamily=FontFamily(Font(Res.font.noto_sans_sc))),
    ) {
        BoxWithConstraints(Modifier.fillMaxSize().background(Paper)) {
            val compact = maxWidth < 760.dp
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(if(compact)20.dp else 40.dp),
                verticalArrangement=Arrangement.spacedBy(22.dp)) {
                Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
                    Image(painterResource(Res.drawable.logo),"Monee Logo",Modifier.size(56.dp))
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(ledger.name,fontSize=26.sp,fontWeight=FontWeight.Bold,color=Pine)
                    }
                }
                Column(verticalArrangement=Arrangement.spacedBy(6.dp)) {
                    Text("${user.provider.displayProvider()} · ${user.label}${if(user.role=="superadmin")" · 超管"else""}",fontSize=12.sp,color=Muted)
                    Row(horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                        TextButton(onClick=onWorkspace,enabled=!working&&!authBusy){Text("账本与账号")}
                        if(user.role=="superadmin")TextButton(onClick=onManage,enabled=!working&&!authBusy){Text("管理白名单")}
                        OutlinedButton(onClick=onLogout,enabled=!working&&!authBusy){Text(if(authBusy)"正在退出…"else"退出登录")}
                    }
                    authError?.let{Text(it,color=MaterialTheme.colors.error)}
                }
                if(connectionError!=null) Surface(color=Color(0xFFFFEBE7),shape=RoundedCornerShape(12.dp)) {
                    Column(Modifier.fillMaxWidth().padding(16.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
                        Text(connectionError.orEmpty(),color=MaterialTheme.colors.error,fontSize=13.sp)
                        if(dashboard!=null) Text("数据未更新",fontSize=12.sp)
                        TextButton(onClick={api.reconnect();refresh++}) { Text("重试") }
                    }
                }
                if (loading || working) LinearProgressIndicator(Modifier.fillMaxWidth(),color=Pine)
                Row(horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                    Button(onClick={panel=if(panel=="import")""else"import";actionError=null},enabled=ledger.role!="viewer"&&dashboard!=null&&!working&&connectionError==null) { Text("导入账单") }
                    OutlinedButton(onClick={panel=if(panel=="manual")""else"manual";if(input.date.isEmpty())input=input.copy(date=dashboard?.today.orEmpty());actionError=null},enabled=ledger.role!="viewer"&&dashboard!=null&&!working&&connectionError==null) { Text("记一笔") }
                    TextButton(onClick={refresh++},enabled=!working) { Text("刷新") }
                }
                if(ledger.role=="viewer")Text("只读账本",color=Muted)
                notice?.let { Text(it,color=Pine) }
                actionError?.let { Text(it,color=MaterialTheme.colors.error) }
                if(panel=="import") {
                    ImportPanel(csv,filename,preview,confirmedSimilar,working,
                        onCSVChange=::changeCSV,
                        onPick={runAction { chooseCSV()?.let { filename=it.name;changeCSV(it.text) } }},
                        onTemplate={runAction { filename="import.csv";changeCSV(api.template(false)) }},
                        onPreview={runAction { preview=api.preview(ledger.id,filename,csv);confirmedSimilar=false }},
                        onConfirmSimilar={confirmedSimilar=it},
                        onCommit={preview?.let { p->runAction { val result=api.commit(ledger.id,p,confirmedSimilar);preview=null;csv="";saved(result.month,"已保存 ${result.added} 笔，跳过 ${result.skipped} 笔重复流水。") } }},
                    )
                }
                if(panel=="manual") {
                    ManualPanel(input,working,onChange={input=it;createKey=requestKey()},onSave={runAction {
                        val created=api.create(ledger.id,input,createKey)
                        input=TransactionInput(date=dashboard?.today.orEmpty());createKey=requestKey()
                        saved(created.date.take(7),"已保存")
                    }})
                }
                dashboard?.let { data ->
                    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),horizontalArrangement=Arrangement.spacedBy(10.dp)) {
                        data.months.forEach { option ->
                            if(option==data.month) Button(onClick={month=option;page=1},shape=RoundedCornerShape(20.dp),elevation=null) { Text(option) }
                            else OutlinedButton(onClick={month=option;page=1},shape=RoundedCornerShape(20.dp)) { Text(option) }
                        }
                    }
                    if(compact) Column(verticalArrangement=Arrangement.spacedBy(12.dp)) {
                        Metric("${data.month} 支出",data.expenseMinor,true,Modifier.fillMaxWidth())
                        Metric("${data.month} 收入",data.incomeMinor,false,Modifier.fillMaxWidth())
                    } else Row(horizontalArrangement=Arrangement.spacedBy(18.dp)) {
                        Metric("${data.month} 支出",data.expenseMinor,true,Modifier.weight(1f))
                        Metric("${data.month} 收入",data.incomeMinor,false,Modifier.weight(1f))
                        Surface(color=Color.White,shape=RoundedCornerShape(18.dp),modifier=Modifier.weight(1f)) {
                            Column(Modifier.padding(24.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
                                Text("本月已入账",fontSize=13.sp,color=Muted)
                                Text("${data.totalCount} 笔",fontSize=29.sp,fontWeight=FontWeight.Bold)
                            }
                        }
                    }
                    if(data.categories.isNotEmpty()) {
                        Surface(color=Color.White,shape=RoundedCornerShape(18.dp)) {
                            Column(Modifier.fillMaxWidth().padding(24.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
                                Text("支出分布",color=Pine,fontWeight=FontWeight.Bold)
                                data.categories.take(8).forEach { item ->
                                    Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                                        Text(item.name,Modifier.width(70.dp),fontSize=12.sp,maxLines=1,overflow=TextOverflow.Ellipsis)
                                        LinearProgressIndicator(item.amountMinor.toLong().toFloat()/data.expenseMinor.toLong().toFloat(),Modifier.weight(1f).height(7.dp),color=Pine,backgroundColor=Line)
                                        Text(formatMoney(item.amountMinor.toLong()),fontSize=12.sp)
                                    }
                                }
                            }
                        }
                    }
                    Column(verticalArrangement=Arrangement.spacedBy(14.dp)) {
                        Text("账单明细 · ${data.filteredCount} 笔",fontWeight=FontWeight.Bold,fontSize=21.sp)
                        OutlinedTextField(query,onValueChange={query=it;page=1},modifier=Modifier.fillMaxWidth(),singleLine=true,
                            label={Text("搜索商户、分类或来源")},shape=RoundedCornerShape(12.dp),
                            trailingIcon={if(query.isNotEmpty())TextButton(onClick={query="";page=1}){Text("清除")}})
                        Card(shape=RoundedCornerShape(16.dp),elevation=0.dp,modifier=Modifier.fillMaxWidth()) {
                            Column {
                                if(data.transactions.isEmpty()) Text(if(query.isBlank())"暂无账单，导入或记一笔。"else"没有找到相关账单",Modifier.padding(24.dp),color=Muted)
                                data.transactions.forEachIndexed { index,item ->
                                    TextButton(onClick={selectedID=if(selectedID==item.id)null else item.id},modifier=Modifier.fillMaxWidth(),contentPadding=PaddingValues(18.dp)) {
                                        Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
                                            if(!compact) Text(item.date,Modifier.width(100.dp),fontSize=13.sp,color=Muted)
                                            Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(5.dp)) {
                                                Text(item.merchant,fontWeight=FontWeight.Medium,color=Ink,maxLines=1,overflow=TextOverflow.Ellipsis)
                                                Text("${item.category} · ${item.source}${if(compact)" · ${item.date}"else""}",fontSize=12.sp,color=Muted)
                                            }
                                            Text(formatMoney(item.amountMinor.toLong()),color=if(item.type=="income")Pine else Ink,fontWeight=FontWeight.SemiBold)
                                        }
                                    }
                                    // Inline details avoid the Compose Wasm popup accessibility issue CMP-10623.
                                    if(selectedID==item.id) Column(Modifier.fillMaxWidth().background(Color(0xFFF0F4EC)).padding(20.dp),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                                        Text("账单详情 · ${item.merchant}",color=Pine,fontWeight=FontWeight.Bold)
                                        Text("日期：${item.date}\n来源：${item.source}\n资金账户：${item.account}（待核实）")
                                        if(item.externalId.isNotEmpty())Text("来源流水号：${item.externalId}")
                                        if(item.note.isNotEmpty())Text("备注：${item.note}")
                                        TextButton(onClick={selectedID=null}){Text("收起详情")}
                                    }
                                    if(index<data.transactions.lastIndex)Divider(color=Line)
                                }
                            }
                        }
                        if(data.filteredCount>data.pageSize) Row(horizontalArrangement=Arrangement.spacedBy(12.dp),verticalAlignment=Alignment.CenterVertically) {
                            OutlinedButton(onClick={page--},enabled=page>1&&!loading){Text("上一页")}
                            Text("第 ${data.page} 页")
                            OutlinedButton(onClick={page++},enabled=data.page*data.pageSize<data.filteredCount&&!loading){Text("下一页")}
                        }
                        if(query.isNotBlank()) Text("搜索仅筛选明细，收支统计不变。",color=Muted,fontSize=12.sp)
                    }
                }
            }
        }
    }
}

@Composable
private fun Metric(label:String,minor:String,emphasized:Boolean,modifier:Modifier) {
    Surface(color=if(emphasized)Pine else Color.White,shape=RoundedCornerShape(18.dp),modifier=modifier) {
        Column(Modifier.padding(24.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
            Text(label,color=if(emphasized)Color(0xFFD6E9DF)else Muted,fontSize=13.sp)
            Text(formatMoney(minor.toLong()),color=if(emphasized)Color.White else Ink,fontSize=29.sp,fontWeight=FontWeight.Bold,maxLines=1)
        }
    }
}
