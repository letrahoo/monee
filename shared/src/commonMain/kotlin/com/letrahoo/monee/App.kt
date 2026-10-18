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
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.input.key.*
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
internal val Muted = Color(0xFF50645A)
private val Paper = Color(0xFFF6F7F2)
internal val Line = Color(0xFFE4E9E1)

private fun requestKey() = List(32) { "0123456789abcdef"[Random.nextInt(16)] }.joinToString("")

@Composable
internal fun LedgerScreen(api:LedgerApi,user:AuthUser,ledger:LedgerInfo,viewState:LedgerViewState,onWorkspace:()->Unit,onAccount:()->Unit,authBusy:Boolean,authError:String?,onLogout:()->Unit,onManage:()->Unit,onAccessLost:()->Unit) {
    val scope = rememberCoroutineScope()
    var dashboard by remember { mutableStateOf<Dashboard?>(null) }
    var loadedRequest by remember {mutableStateOf<DashboardRequest?>(null)}
    var lastResolvedMonth by remember {mutableStateOf("")}
    var lastToday by remember {mutableStateOf("")}
    var filterOptions by remember {mutableStateOf(DashboardFilterOptions())}
    var optionsMonth by remember {mutableStateOf("")}
    var restoreScroll by remember(viewState.tab) {mutableStateOf(true)}
    val monthFocus = remember {FocusRequester()}
    var pendingMonthFocus by remember {mutableStateOf(false)}
    var month by viewState::month
    var query by viewState::query
    var page by viewState::page
    var annual by remember { mutableStateOf<AnnualReport?>(null) }
    var annualLoading by remember { mutableStateOf(false) }
    var annualError by remember { mutableStateOf<String?>(null) }
    val effectiveMonth = month.ifBlank { lastResolvedMonth }
    val reportYear = effectiveMonth.take(4)
    val detailFilters = if(viewState.tab==WorkbenchTab.Details)viewState.filters else DashboardFilters()
    val detailQuery = if(viewState.tab==WorkbenchTab.Details)query else ""
    val detailPage = if(viewState.tab==WorkbenchTab.Details)page else 1
    val dashboardRequest=DashboardRequest(month,detailQuery,detailPage,detailFilters)
    var refresh by remember { mutableStateOf(0) }
    var loading by remember { mutableStateOf(true) }
    var working by remember { mutableStateOf(false) }
    var connectionError by remember { mutableStateOf<String?>(null) }
    var actionError by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var panel by remember { mutableStateOf("") }
    var sourceLinks by remember { mutableStateOf<List<TransactionSource>>(emptyList()) }
    var sourceID by remember { mutableStateOf("") }
    var revisions by remember { mutableStateOf<List<AnnotationRevision>>(emptyList()) }
    var corrections by remember { mutableStateOf<List<CorrectionRevision>>(emptyList()) }
    var revisionID by remember { mutableStateOf("") }
    var selectedID by viewState::selectedId
    var csv by remember { mutableStateOf("") }
    var filename by remember { mutableStateOf("import.csv") }
    var undoPreview by remember { mutableStateOf<ImportUndoPreview?>(null) }
    var undoFilename by remember { mutableStateOf("") }
    var reviewQueue by remember { mutableStateOf<ReviewQueue?>(null) }
    var reviewFormat by remember { mutableStateOf("") }
    var importHistory by remember { mutableStateOf<ImportHistory?>(null) }
    var importFormat by remember { mutableStateOf("alipay") }
    val nativeImport = importFormat != "standard"
    var nativeContent by remember { mutableStateOf("") }
    var sourceAccount by remember { mutableStateOf("我的支付宝") }
    var preview by remember { mutableStateOf<ImportPreview?>(null) }
    var redaction by remember { mutableStateOf<RedactionPreview?>(null) }
    var confirmedSimilar by remember { mutableStateOf(false) }
    var input by remember { mutableStateOf(TransactionInput()) }
    var createKey by remember { mutableStateOf(requestKey()) }
    var refundSummary by remember { mutableStateOf<RefundSummary?>(null) }
    // Keep uncertain writes above the polling/list UI: version refresh or closing
    // the panel must not silently generate a second idempotency key.
    var refundSubmission by remember { mutableStateOf(api.pendingRefund(ledger.id)) }

    LaunchedEffect(dashboardRequest, refresh) {
        loading = true
        if(loadedRequest!=dashboardRequest)dashboard=null
        try {
            if (dashboardRequest.query.isNotEmpty()) delay(250)
            val result=api.dashboard(ledger.id,dashboardRequest.month,dashboardRequest.query,dashboardRequest.page,dashboardRequest.filters)
            loadedRequest=dashboardRequest;dashboard=result
            lastResolvedMonth=result.month;lastToday=result.today;filterOptions=result.filterOptions;optionsMonth=result.month
            connectionError = null
        } catch (e: CancellationException) { throw e
        } catch (e: Exception) { if(e is LedgerException&&e.accessLost)onAccessLost() else {dashboard=null;connectionError = e.message ?: "本地服务暂不可用"}
        } finally { loading = false }
    }
    LaunchedEffect(reportYear, viewState.tab, viewState.monthPanel, refresh) {
        if(reportYear.isEmpty() || (viewState.tab!=WorkbenchTab.Statistics && !viewState.monthPanel)) return@LaunchedEffect
        annualLoading=true
        try {
            val report=api.annual(ledger.id,reportYear)
            annual=report;annualError=null
        } catch(e:CancellationException){throw e
        } catch(e:Exception){annual=null;if(e is LedgerException&&e.accessLost)onAccessLost()else annualError=e.message
        } finally {annualLoading=false}
    }
    fun scrollReady():Boolean = if(viewState.tab==WorkbenchTab.Statistics)annual?.year?.toString()==reportYear&&!annualLoading else dashboard!=null&&!loading
    LaunchedEffect(viewState.tab,reportYear,dashboard,annual,loading,annualLoading) {
        if(restoreScroll&&scrollReady()){
            val targetTab=viewState.tab
            withFrameNanos { } // Restore only after this tab's loaded content is measured.
            if(targetTab==viewState.tab&&scrollReady()){
                viewState.scrolls.getValue(targetTab).scrollTo(viewState.scrollOffsets[targetTab] ?: 0)
                restoreScroll=false
            }
        }
    }
    LaunchedEffect(Unit) {
        while (true) { delay(5000); if (!working && !loading && !annualLoading) refresh++ }
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
        viewState.selectMonth(savedMonth);viewState.clearFilters();viewState.selectTab(WorkbenchTab.Details);panel="";selectedID=null
        notice = message; refresh++
    }

    fun monthToolbarMounted() = panel!="review"&&viewState.tab!=WorkbenchTab.Mine&&validWorkbenchMonth(effectiveMonth)
    // A navigation/editor change expires an earlier close-panel focus intent.
    LaunchedEffect(viewState.tab,panel) { pendingMonthFocus=false }
    LaunchedEffect(pendingMonthFocus,viewState.tab,panel,effectiveMonth,working,viewState.monthPanel) {
        if(viewState.monthPanel){pendingMonthFocus=false;return@LaunchedEffect}
        if(pendingMonthFocus&&monthToolbarMounted()&&!working){
            // Bounded frame opportunities, not a polling loop or a guessed delay.
            repeat(2) {
                withFrameNanos { }
                if(!pendingMonthFocus||viewState.monthPanel||!monthToolbarMounted()||working)return@LaunchedEffect
                if(monthFocus.requestFocus(FocusDirection.Enter)){pendingMonthFocus=false;return@LaunchedEffect}
            }
        }
    }
    fun closeMonthPanel(){viewState.monthPanel=false;pendingMonthFocus=true}
    fun chooseMonth(value:String){viewState.selectMonth(value);selectedID=null;pendingMonthFocus=true}

    MaterialTheme(
        colors = lightColors(primary=Pine,secondary=Color(0xFFE4B940),background=Paper,surface=Color.White,onBackground=Ink,onSurface=Ink),
        typography = Typography(defaultFontFamily=FontFamily(Font(Res.font.noto_sans_sc))),
        shapes = Shapes(small=RoundedCornerShape(10.dp),medium=RoundedCornerShape(16.dp),large=RoundedCornerShape(24.dp)),
    ) {
        BoxWithConstraints(Modifier.fillMaxSize().background(Paper).onPreviewKeyEvent { event ->
            if(viewState.monthPanel&&event.type==KeyEventType.KeyDown&&event.key==Key.Escape){closeMonthPanel();true}else false
        },contentAlignment=Alignment.TopCenter) {
            val compact = maxWidth < 760.dp
            Column(Modifier.widthIn(max=1200.dp).fillMaxSize().padding(horizontal=if(compact)16.dp else 32.dp),
                verticalArrangement=Arrangement.spacedBy(12.dp)) {
                Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.SpaceBetween) {
                    BrandHeader()
                    Row(horizontalArrangement=Arrangement.spacedBy(4.dp)) {
                        TextButton(onClick={viewState.saveScroll();onWorkspace()},enabled=!working&&!authBusy){Text(ledger.name,maxLines=1,overflow=TextOverflow.Ellipsis)}
                    }
                }
                WorkbenchNavigation(viewState.tab,working||authBusy) { next ->
                    if(next!=viewState.tab){viewState.selectTab(next);panel="";viewState.monthPanel=false;actionError=null;notice=null}
                }
                Column(Modifier.weight(1f).fillMaxWidth().verticalScroll(viewState.scrolls.getValue(viewState.tab)).padding(vertical=12.dp),
                    verticalArrangement=Arrangement.spacedBy(18.dp)) {
                authError?.let{Text(it,color=MaterialTheme.colors.error)}
                if(connectionError!=null) Surface(color=Color(0xFFFFEBE7),shape=RoundedCornerShape(12.dp)) {
                    Column(Modifier.fillMaxWidth().padding(16.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
                        Text(connectionError.orEmpty(),color=MaterialTheme.colors.error,fontSize=13.sp)
                        if(dashboard!=null) Text("数据未更新",fontSize=12.sp)
                        TextButton(onClick={api.reconnect();refresh++}) { Text("重试") }
                    }
                }
                if ((loading && dashboard==null) || working) LinearProgressIndicator(Modifier.fillMaxWidth(),color=Pine)
                if(viewState.tab==WorkbenchTab.Mine) {
                    Text("我的",fontSize=24.sp,fontWeight=FontWeight.Bold,color=Ink)
                    SettingsCard("账本与账号") {
                        Text(ledger.name,color=Ink)
                        FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                            OutlinedButton(onClick={viewState.saveScroll();onWorkspace()},enabled=!working&&!authBusy){Text("账本与成员")}
                            TextButton(onClick={viewState.saveScroll();onAccount()},enabled=!working&&!authBusy){Text("账号设置")}
                            if(user.role=="superadmin")TextButton(onClick={viewState.saveScroll();onManage()},enabled=!working&&!authBusy){Text("系统准入")}
                            TextButton(onClick={viewState.saveScroll();onLogout()},enabled=!working&&!authBusy){Text("退出登录")}
                        }
                    }
                    SettingsCard("数据来源") {
                        Text("支付宝、微信和标准 CSV 文件补充",color=Ink)
                        FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                            OutlinedButton(onClick={panel=if(panel=="import")""else"import";actionError=null},enabled=ledger.role!="viewer"&&dashboard!=null&&!working&&connectionError==null){Text("文件补充")}
                            TextButton(onClick={runAction {importHistory=api.importHistory(ledger.id,1);panel="history"}},enabled=!working){Text("导入记录与原文")}
                            TextButton(onClick={runAction {reviewQueue=api.reviewQueue(ledger.id,1,reviewFormat);panel="review"}},enabled=!working){Text("查看待核实")}
                        }
                    }
                }
                if(viewState.tab==WorkbenchTab.Details) FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick={panel=if(panel=="manual")""else"manual";if(input.date.isEmpty())input=input.copy(date=dashboard?.today.orEmpty());actionError=null},enabled=ledger.role!="viewer"&&dashboard!=null&&!working&&connectionError==null){Text("补记一笔")}
                    TextButton(onClick={runAction {reviewQueue=api.reviewQueue(ledger.id,1,reviewFormat);panel="review"}},enabled=!working){Text("查看待核实")}
                }
                TextButton(onClick={if(panel=="review")runAction {reviewQueue=api.reviewQueue(ledger.id,reviewQueue?.page ?: 1,reviewFormat)}else refresh++},enabled=!working){Text("刷新")}
                if(ledger.role=="viewer")Text("只读账本",color=Muted)
                notice?.let { Text(it,color=Pine) }
                actionError?.let { Text(it,color=MaterialTheme.colors.error) }
                if(refundSubmission!=null&&panel!="refund")TextButton(onClick={runAction {
                    refundSummary=api.refunds(ledger.id,refundSubmission!!.originalId);viewState.selectTab(WorkbenchTab.Details);panel="refund"
                }},enabled=!working){Text("继续确认上次退款")}
                if(panel=="refund")refundSummary?.let { summary ->
                    RefundPanel(summary,dashboard?.today.orEmpty(),ledger.role=="viewer",working,refundSubmission,
                        onSubmit={change->runAction {
                            val submission=api.prepareRefund(ledger.id,summary.original.id,change,::requestKey).also { refundSubmission=it }
                            try {
                                val result=api.createRefund(ledger.id,submission)
                                refundSubmission=null;refundSummary=null
                                saved(result.date.take(7),"退款已保存")
                            } catch(e:LedgerException) {
                                refundSubmission=api.pendingRefund(ledger.id)
                                throw e
                            }
                        }},
                        onRefresh={runAction {refundSummary=api.refunds(ledger.id,summary.original.id)}},
                        onClose={panel=""})
                }
                if(panel=="import") {
                    TextButton(onClick={panel=""},enabled=!working){Text("收起导入")}
                    ImportPanel(csv,filename,preview,confirmedSimilar,working||ledger.role=="viewer",
                        format=importFormat,hasNativeFile=nativeContent.isNotEmpty(),sourceAccount=sourceAccount,
                        onMode={redaction=null;importFormat=it;sourceAccount=if(it=="wechat")"我的微信"else"我的支付宝";preview=null;csv="";nativeContent="";filename="import.csv";confirmedSimilar=false},
                        onAccount={redaction=null;sourceAccount=it;preview=null;confirmedSimilar=false},
                        onCSVChange=::changeCSV,
                        onPick={runAction {
                            if(nativeImport) chooseNativeBill(importFormat)?.let { filename=it.name;nativeContent=it.contentBase64;preview=null;redaction=null;confirmedSimilar=false }
                            else chooseCSV()?.let { filename=it.name;changeCSV(it.text) }
                        }},
                        onTemplate={runAction { filename="import.csv";changeCSV(api.template(false)) }},
                        onPreview={runAction { redaction=null;preview=if(nativeImport)api.previewNative(ledger.id,importFormat,filename,nativeContent,sourceAccount) else api.preview(ledger.id,filename,csv);confirmedSimilar=false }},
                        onConfirmSimilar={confirmedSimilar=it},
                        onCommit={preview?.let { p->runAction { val result=api.commit(ledger.id,p,confirmedSimilar);preview=p.copy(alreadyCommitted=true);csv="";nativeContent="";month=result.month;query="";page=1;refresh++
                            notice="已保存 ${result.added} 笔，跳过 ${result.skipped} 笔重复流水。"+if(result.pending>0)"另有 ${result.pending} 笔待核实，未计入收支。"else"" } }},
                    )
                    preview?.takeIf { it.alreadyCommitted }?.let { completed ->
                        FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick={runAction {importHistory=api.importHistory(ledger.id,1);panel="history"}},enabled=!working){Text("返回导入记录")}
                            TextButton(onClick={runAction {reviewQueue=api.reviewQueue(ledger.id,1,reviewFormat);panel="review"}},enabled=!working){Text("查看待核实")}
                            TextButton(onClick={preview=null;csv="";nativeContent="";redaction=null;confirmedSimilar=false},enabled=!working&&ledger.role!="viewer"){Text("补充另一个文件")}
                        }
                        if(ledger.role!="viewer") {
                            var showBatchActions by remember(completed.id) {mutableStateOf(false)}
                            TextButton(onClick={showBatchActions=!showBatchActions},enabled=!working){Text(if(showBatchActions)"收起批次操作"else"更多批次操作")}
                            if(showBatchActions)TextButton(onClick={runAction {
                                val current=api.importUndoPreview(ledger.id,completed.id)
                                undoPreview=if(current.state=="undone")api.importUndoPreview(ledger.id,completed.id,true)else current
                                undoFilename=filename;panel="undo"
                            }},enabled=!working){Text(if(completed.undone)"预览恢复影响"else"预览撤销影响")}
                        }
                    }
                    preview?.takeIf { it.id.isNotEmpty() && it.format in listOf("alipay","wechat") && it.errors.isEmpty() }?.let { imported ->
                        TextButton(onClick={runAction { redaction=null;redaction=api.redactionPreview(ledger.id,imported.id) }},enabled=!working) { Text("查看脱敏预览") }
                        redaction?.takeIf { it.importId==imported.id }?.let { result ->
                            RedactionPreviewPanel(result,onClose={redaction=null})
                        }
                    }
                }
                if(panel=="review") reviewQueue?.let { queue ->
                    ReviewQueuePanel(queue,working,
                        onFormat={format->runAction {val next=api.reviewQueue(ledger.id,1,format);reviewQueue=next;reviewFormat=format}},
                        onPage={next->runAction {reviewQueue=api.reviewQueue(ledger.id,next,reviewFormat)}},
                        onClose={panel="";actionError=null},
                        onOpenBatch={id->runAction {
                            val detail=api.importDetail(ledger.id,id)
                            preview=detail.preview;filename=detail.preview.filename;importFormat=detail.preview.format
                            nativeContent="";csv="";sourceAccount=detail.preview.sourceAccount;confirmedSimilar=false;panel="import"
                        }})
                }
                if(panel=="history") importHistory?.let { history ->
                    ImportHistoryPanel(history,working,canManage=ledger.role!="viewer",
                        onManage={item->runAction {
                            val first=api.importUndoPreview(ledger.id,item.id)
                            undoPreview=if(first.state=="undone")api.importUndoPreview(ledger.id,item.id,true) else first
                            undoFilename=item.filename;panel="undo"
                        }},
                        onPage={next->runAction {importHistory=api.importHistory(ledger.id,next)}},
                        onOpen={id->runAction {
                            val detail=api.importDetail(ledger.id,id)
                            redaction=null;preview=detail.preview;filename=detail.preview.filename;importFormat=detail.preview.format
                            nativeContent="";csv="";sourceAccount=detail.preview.sourceAccount.ifBlank { "我的支付宝" };confirmedSimilar=false;panel="import"
                        }})
                }
                if(panel=="undo") undoPreview?.let { candidate ->
                    ImportUndoPanel(undoFilename,candidate,working,
                        onRefresh={runAction {
                            val latest=api.importUndoPreview(ledger.id,candidate.importId)
                            undoPreview=if(latest.state=="undone")api.importUndoPreview(ledger.id,candidate.importId,true)else latest
                        }},
                        onCancel={panel="history";undoPreview=null;actionError=null},
                        onConfirm={runAction {
                            val result=api.changeImportState(ledger.id,candidate)
                            notice=if(result.alreadyApplied)"该操作已处理，账单未重复变更"else"已${if(candidate.action=="restore")"恢复"else"撤销"} ${result.changeCount} 笔，保留 ${result.preservedCount} 笔"
                            importHistory=api.importHistory(ledger.id,1);undoPreview=null;preview=null;panel="history";selectedID=null;refresh++
                        }})
                }
                if(panel=="manual") {
                    TextButton(onClick={panel=""},enabled=!working){Text("收起记账")}
                    ManualPanel(input,working,onChange={input=it;createKey=requestKey()},onSave={runAction {
                        val created=api.create(ledger.id,input,createKey)
                        input=TransactionInput(date=dashboard?.today.orEmpty());createKey=requestKey()
                        saved(created.date.take(7),"已保存")
                    }})
                }
                if(panel!="review"&&viewState.tab==WorkbenchTab.Details) {
                    // Keep controls mounted while results load; typing must not lose focus.
                    DashboardFilterPanel(if(optionsMonth==effectiveMonth)filterOptions else DashboardFilterOptions(),viewState.filters,!working,
                        onChange={viewState.filter(it);selectedID=null},onClear={viewState.clearFilters();selectedID=null})
                    OutlinedTextField(query,onValueChange={viewState.search(it);selectedID=null},modifier=Modifier.fillMaxWidth(),singleLine=true,enabled=!working,
                        label={Text("搜索商户、分类、来源、账户或备注")},shape=RoundedCornerShape(12.dp),
                        trailingIcon={if(query.isNotEmpty())TextButton(onClick={viewState.search("");selectedID=null}){Text("清除")}})
                }
                if(panel!="review" && viewState.tab!=WorkbenchTab.Mine && validWorkbenchMonth(effectiveMonth)) {
                    MonthToolbar(effectiveMonth,lastToday,working,
                        onMonth=::chooseMonth,onOpen={pendingMonthFocus=false;viewState.monthPanel=!viewState.monthPanel},monthFocus=monthFocus)
                    if(viewState.monthPanel) {
                        if(annualLoading)LinearProgressIndicator(Modifier.fillMaxWidth())
                        annualError?.let{Text(it,color=MaterialTheme.colors.error);TextButton(onClick={refresh++}){Text("重试年度数据")}}
                        annual?.takeIf{it.year.toString()==reportYear}?.let { report ->
                            MonthSelectionPanel(report,effectiveMonth,annualLoading,disabled=working,
                                onMonth=::chooseMonth,
                                onYear={chooseMonth("$it-${effectiveMonth.takeLast(2)}")},
                                onClose=::closeMonthPanel)
                        }
                    }
                    if(viewState.tab==WorkbenchTab.Statistics) {
                        if(annualLoading)LinearProgressIndicator(Modifier.fillMaxWidth())
                        annualError?.let{Text(it,color=MaterialTheme.colors.error);TextButton(onClick={refresh++}){Text("重试年度统计")}}
                        annual?.takeIf{it.year.toString()==reportYear}?.let { report ->
                            AnnualStatisticsPanel(report,effectiveMonth,viewState.statistics,onMonth={viewState.selectMonth(it);selectedID=null})
                        }
                    }
                }
                if(panel!="review" && viewState.tab in listOf(WorkbenchTab.Overview,WorkbenchTab.Details)) dashboard?.let { data ->
                    if(compact) Column(verticalArrangement=Arrangement.spacedBy(12.dp)) {
                        Metric("${data.month} 净支出",data.expenseMinor,true,Modifier.fillMaxWidth())
                        Metric("${data.month} 收入",data.incomeMinor,false,Modifier.fillMaxWidth())
                    } else Row(horizontalArrangement=Arrangement.spacedBy(18.dp)) {
                        Metric("${data.month} 净支出",data.expenseMinor,true,Modifier.weight(1f))
                        Metric("${data.month} 收入",data.incomeMinor,false,Modifier.weight(1f))
                        Surface(color=Color.White,shape=RoundedCornerShape(18.dp),modifier=Modifier.weight(1f)) {
                            Column(Modifier.padding(24.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
                                Text("${data.month} 已入账",fontSize=13.sp,color=Muted)
                                Text("${data.totalCount} 笔",fontSize=29.sp,fontWeight=FontWeight.Bold)
                            }
                        }
                    }
                    Text("消费毛额 ${formatMoney(data.grossExpenseMinor.toLong())} · 退款 ${formatMoney(data.refundMinor.toLong())} · 净支出 ${formatMoney(data.expenseMinor.toLong())}",color=Muted)
                    if(viewState.tab==WorkbenchTab.Overview) {
                        if(data.totalCount==0)SettingsCard("${data.month} 暂无已入账记录") {
                            data.months.firstOrNull{it!=data.month&&it!=data.today.take(7)}?.let { latest ->
                                TextButton(onClick={viewState.selectMonth(latest)}){Text("查看 $latest")}
                            }
                            TextButton(onClick={viewState.selectTab(WorkbenchTab.Mine)}){Text("查看数据来源与文件补充")}
                        }
                        GrossCategoryPanel(data.categories,data.grossExpenseMinor,onCategory={category->
                            viewState.filter(DashboardFilters(category=category));viewState.query="";viewState.selectTab(WorkbenchTab.Details)
                        })
                        SettingsCard("最近明细 · ${data.month}") {
                            data.transactions.take(5).forEach { item ->
                                TextButton(onClick={viewState.clearFilters();viewState.selectTab(WorkbenchTab.Details);selectedID=item.id},modifier=Modifier.fillMaxWidth()) {
                                    Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceBetween) {
                                        Text("${item.date} · ${item.merchant}",Modifier.weight(1f),maxLines=1,overflow=TextOverflow.Ellipsis,color=Ink)
                                        Text(if(item.type=="refund")"退款 ${formatMoney(item.amountMinor.toLong())}"else formatTransactionMoney(item.amountMinor.toLong()),color=Ink)
                                    }
                                }
                            }
                            TextButton(onClick={viewState.clearFilters();viewState.selectTab(WorkbenchTab.Details)}){Text("查看全部明细")}
                        }
                    }
                    if(viewState.tab==WorkbenchTab.Details) Column(verticalArrangement=Arrangement.spacedBy(14.dp)) {
                        Text("账单明细 · ${data.filteredCount} 笔",fontWeight=FontWeight.Bold,fontSize=21.sp)
                        Card(shape=RoundedCornerShape(16.dp),elevation=0.dp,modifier=Modifier.fillMaxWidth()) {
                            Column {
                                if(data.transactions.isEmpty()) Text(if(query.isNotBlank()||viewState.filters.active)"没有符合筛选条件的明细"else"${data.month} 暂无已入账记录",Modifier.padding(24.dp),color=Muted)
                                data.transactions.forEachIndexed { index,item ->
                                    TextButton(onClick={selectedID=if(selectedID==item.id)null else item.id},modifier=Modifier.fillMaxWidth(),contentPadding=PaddingValues(18.dp)) {
                                        Row(Modifier.fillMaxWidth(),verticalAlignment=Alignment.CenterVertically) {
                                            if(!compact) Text(item.date,Modifier.width(100.dp),fontSize=13.sp,color=Muted)
                                            Column(Modifier.weight(1f),verticalArrangement=Arrangement.spacedBy(5.dp)) {
                                                Text(item.merchant,fontWeight=FontWeight.Medium,color=Ink,maxLines=1,overflow=TextOverflow.Ellipsis)
                                                Text("${item.category} · ${item.source}${if(compact)" · ${item.date}"else""}",fontSize=12.sp,color=Muted)
                                            }
                                            Text(formatTransactionMoney(item.amountMinor.toLong()),color=if(item.type=="income")Pine else Ink,fontWeight=FontWeight.SemiBold)
                                        }
                                    }
                                    // Inline details avoid the Compose Wasm popup accessibility issue CMP-10623.
                                    if(selectedID==item.id) Column(Modifier.fillMaxWidth().background(Color(0xFFF0F4EC)).padding(20.dp),verticalArrangement=Arrangement.spacedBy(8.dp)) {
                                        Text("账单详情 · ${item.merchant}",color=Pine,fontWeight=FontWeight.Bold)
                                        Text("日期：${item.date}\n来源：${item.source}\n资金账户：${item.account}")
                                        if(item.externalId.isNotEmpty()&&!item.externalId.contains("-native-v1:"))Text("来源流水号：${item.externalId}")
                                        if(item.note.isNotEmpty())Text("备注：${item.note}")
                                        if(item.type=="refund")Text("消费退款（不计入收入）",color=Pine)
                                        if(item.type=="expense"||item.type=="refund")TextButton(onClick={runAction {
                                            refundSummary=api.refunds(ledger.id,item.id);panel="refund"
                                        }},enabled=!working&&(refundSubmission==null||refundSubmission?.originalId==(item.refundOf.ifBlank { item.id }))){Text(if(item.type=="refund")"查看原消费与退款"else"退款记录 / 登记退款")}
                                        TextButton(onClick={runAction {sourceLinks=api.transactionSources(ledger.id,item.id);sourceID=item.id}},enabled=!working){Text("来源批次")}
                                        if(sourceID==item.id) {
                                            if(sourceLinks.isEmpty())Text("这笔交易没有导入来源",fontSize=12.sp,color=Muted)
                                            sourceLinks.forEach { link -> TextButton(onClick={runAction {
                                                val detail=api.importDetail(ledger.id,link.importId)
                                                redaction=null;preview=detail.preview;filename=detail.preview.filename;importFormat=detail.preview.format
                                                sourceAccount=detail.preview.sourceAccount;nativeContent="";csv="";confirmedSimilar=false;viewState.selectTab(WorkbenchTab.Mine);panel="import"
                                            }},enabled=!working){Text("${link.filename} · 第 ${link.line} 行")} }
                                        }
                                        var editMetadata by remember(item.id) {mutableStateOf(false)}
                                        var editAccounting by remember(item.id) {mutableStateOf(false)}
                                        if(ledger.role!="viewer")TextButton(onClick={editMetadata=!editMetadata},enabled=!working){Text(if(editMetadata)"收起分类与备注"else"修改分类与备注")}
                                        if(ledger.role!="viewer"&&editMetadata) AnnotationEditor(item,working,onSave={category,note->runAction {
                                            api.annotate(ledger.id,item.id,AnnotationInput(item.version,category,note));notice="分类与备注已保存";refresh++;revisionID=""
                                        }})
                                        if(ledger.role!="viewer"&&item.type in listOf("income","expense"))TextButton(onClick={editAccounting=!editAccounting},enabled=!working){Text(if(editAccounting)"收起金额更正"else"更正金额与交易性质")}
                                        if(ledger.role!="viewer"&&item.type in listOf("income","expense")&&editAccounting) CorrectionEditor(item,working,
                                            onPreview={change,accept->runAction { accept(api.previewCorrection(ledger.id,item.id,change)) }},
                                            onConfirm={change->runAction { api.correct(ledger.id,item.id,change);notice="金额与收支性质已更正";refresh++;revisionID="" }})
                                        TextButton(onClick={runAction { revisions=api.annotationHistory(ledger.id,item.id);corrections=api.correctionHistory(ledger.id,item.id);revisionID=item.id }},enabled=!working){Text("修改记录")}
                                        if(revisionID==item.id) {
                                            if(revisions.isEmpty()&&corrections.isEmpty())Text("暂无修改记录",fontSize=12.sp,color=Muted)
                                            corrections.forEach { revision -> Text("${revision.createdAt.take(19)} · ${if(revision.before.type=="income")"收入"else"支出"} ${formatMoney(revision.before.amountMinor.toLong())} → ${if(revision.after.type=="income")"收入"else"支出"} ${formatMoney(revision.after.amountMinor.toLong())}\n原因：${revision.reason}",fontSize=12.sp,color=Muted) }
                                            revisions.forEach { revision -> Text("${revision.createdAt.take(19)} · 分类：${revision.before.category} → ${revision.after.category}\n备注：${revision.before.note.ifBlank { "无" }} → ${revision.after.note.ifBlank { "无" }}",fontSize=12.sp,color=Muted) }
                                        }
                                        TextButton(onClick={selectedID=null}){Text("收起详情")}
                                    }
                                    if(index<data.transactions.lastIndex)Divider(color=Line)
                                }
                            }
                        }
                        if(data.filteredCount>data.pageSize) Row(horizontalArrangement=Arrangement.spacedBy(12.dp),verticalAlignment=Alignment.CenterVertically) {
                            OutlinedButton(onClick={page--},enabled=page>1&&!loading){Text("上一页")}
                            Text("${data.page} / ${(data.filteredCount+data.pageSize-1)/data.pageSize} 页")
                            OutlinedButton(onClick={page++},enabled=data.page*data.pageSize<data.filteredCount&&!loading){Text("下一页")}
                        }
                        if(query.isNotBlank()||viewState.filters.active) Text("筛选仅影响明细与条数，以上收支总计仍为完整月份。",color=Muted,fontSize=12.sp)
                    }
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
