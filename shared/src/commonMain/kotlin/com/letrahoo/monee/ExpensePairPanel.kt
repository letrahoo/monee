package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.letrahoo.monee.data.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private data class ExpensePairUiLoadedDetail(val ticket:ExpensePairUiDetailTicket,val value:ExpensePairDetail)
private data class ExpensePairUiLoadedPreview(val ticket:ExpensePairUiPreviewTicket,val value:ExpensePairPreview)
private data class ExpensePairUiSources(val primary:List<TransactionSource>,val secondary:List<TransactionSource>)

@Composable
internal fun ExpensePairPanel(api:LedgerApi,userId:String,ledgerId:String,readOnly:Boolean,dashboard:Dashboard?,
    initialTransactionId:String?,onChanged:()->Unit,onAccessLost:()->Unit,onClose:()->Unit) {
    // A new user/ledger gets a new coroutine scope and no old UI callbacks.
    key(api,userId,ledgerId) {
        ExpensePairPanelContent(api,userId,ledgerId,readOnly,dashboard,initialTransactionId,onChanged,onAccessLost,onClose)
    }
}

@Composable
private fun ExpensePairPanelContent(api:LedgerApi,userId:String,ledgerId:String,readOnly:Boolean,dashboard:Dashboard?,
    initialTransactionId:String?,onChanged:()->Unit,onAccessLost:()->Unit,onClose:()->Unit) {
    val scope=rememberCoroutineScope()
    val changed by rememberUpdatedState(onChanged)
    val accessLost by rememberUpdatedState(onAccessLost)
    val latestReadOnly by rememberUpdatedState(readOnly)
    val snapshot=expensePairUiSnapshot(dashboard,ledgerId)
    val latestSnapshot by rememberUpdatedState(snapshot)
    val dashboardVersion=dashboard?.takeIf { it.ledgerId==ledgerId }?.version
    val latestVersion by rememberUpdatedState(dashboardVersion)
    val candidates=expensePairUiCandidates(dashboard,ledgerId)
    var primaryId by remember { mutableStateOf<String?>(null) }
    var secondaryId by remember { mutableStateOf<String?>(null) }
    var reason by remember { mutableStateOf("") }
    var preview by remember { mutableStateOf<ExpensePairUiLoadedPreview?>(null) }
    var pending by remember { mutableStateOf(api.pendingExpensePair(ledgerId)) }
    var busy by remember { mutableStateOf(false) }
    var detailLoading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var detailTarget by remember { mutableStateOf(initialTransactionId) }
    var detailGeneration by remember { mutableStateOf(0) }
    var loadedDetail by remember { mutableStateOf<ExpensePairUiLoadedDetail?>(null) }
    var sourceResults by remember { mutableStateOf<Map<String,ExpensePairUiSources>>(emptyMap()) }
    var withdrawDecisionId by remember { mutableStateOf<String?>(null) }
    var withdrawReason by remember { mutableStateOf("") }
    var withdrawAccepted by remember { mutableStateOf(false) }
    var showPendingScope by remember { mutableStateOf(false) }

    fun detailCurrent(ticket:ExpensePairUiDetailTicket)=ticket.current(detailTarget,latestVersion,detailGeneration)
    val detail=loadedDetail?.takeIf { detailCurrent(it.ticket) }?.value
    val visiblePreview=preview?.takeIf { it.ticket.current(snapshot,primaryId,secondaryId,reason) }
    val pendingScopeMatches=expensePairUiPendingMatches(pending,userId,ledgerId)
    val canWrite=expensePairUiCanWrite(readOnly,busy,pending,pendingScopeMatches)

    LaunchedEffect(initialTransactionId) { detailTarget=initialTransactionId }
    LaunchedEffect(snapshot) {
        preview=null
        if(candidates.none { it.id==primaryId })primaryId=null
        if(candidates.none { it.id==secondaryId })secondaryId=null
        withdrawAccepted=false
    }
    LaunchedEffect(detailTarget,dashboardVersion,detailGeneration) {
        loadedDetail=null;sourceResults=emptyMap();withdrawAccepted=false;detailLoading=false
        val id=detailTarget ?: return@LaunchedEffect
        val ticket=ExpensePairUiDetailTicket(id,dashboardVersion,detailGeneration)
        detailLoading=true
        try {
            val result=api.expensePairs(ledgerId,id)
            if(detailCurrent(ticket)) { loadedDetail=ExpensePairUiLoadedDetail(ticket,result);error=null }
        } catch(e:CancellationException) { throw e
        } catch(e:Exception) {
            if(detailCurrent(ticket)) {
                if(e is LedgerException && e.accessLost)accessLost() else error=expensePairUiError(e)
            }
        } finally { if(detailCurrent(ticket))detailLoading=false }
    }

    fun action(block:suspend ()->Unit) {
        if(busy)return
        busy=true;error=null;notice=null
        scope.launch {
            try { block()
            } catch(e:CancellationException) { throw e
            } catch(e:Exception) {
                if(e is LedgerException && e.accessLost)accessLost() else error=expensePairUiError(e)
                if(e is LedgerException && e.code in listOf("invalid","conflict")) {
                    preview=null;detailGeneration++;changed()
                }
            } finally { busy=false }
        }
    }

    suspend fun submit(submission:ExpensePairSubmission) {
        // Never prepare again when reconciling an unknown result. This exact object
        // is the API's registration, including its original versions/input/key.
        pending=submission
        if(latestReadOnly || !expensePairUiPendingMatches(submission,userId,ledgerId))
            throw LedgerException("账号或权限已变化，请返回原账号与账本核实。","expense_pair_scope_changed")
        val snapshotAtSend=latestSnapshot
        val targetAtSend=detailTarget
        val detailAtSend=detailGeneration
        try {
            val result=when(submission) {
                is ExpensePairConfirmSubmission -> api.confirmExpensePair(ledgerId,submission)
                is ExpensePairWithdrawSubmission -> api.withdrawExpensePair(ledgerId,submission)
            }
            pending=null
            notice=if(result.status=="active")"已关联：保留主记录统计，原始凭证均已保留。"else"已撤回关联；刷新后两条支出均恢复统计。"
            if(snapshotAtSend==latestSnapshot && targetAtSend==detailTarget && detailAtSend==detailGeneration) {
                preview=null;withdrawAccepted=false;withdrawDecisionId=null
                detailTarget=result.primaryId;detailGeneration++
            }
            changed()
        } catch(e:CancellationException) { throw e
        } catch(e:Exception) {
            // Only the API's definite current-scope invalid/conflict can release
            // this lock. A changed scope may hide the old registration in its map.
            if(e is LedgerException && e.code in listOf("invalid","conflict") && api.pendingExpensePair(ledgerId)!==submission)pending=null
            throw e
        }
    }

    Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp)) {
        Text("关联同笔消费",color=Pine)
        Text("仅支持同一实际资金账户的同笔支出，明确保留一条主记录统计；原始交易与两份凭证都保留。账户名称相同不代表同一账户，是否可关联由服务端核验。",color=Muted)
        if(readOnly)Text("当前为只读权限，可查看关联历史与两侧来源，不能确认、撤回或重试写入。",color=Muted)
        if(busy || detailLoading)LinearProgressIndicator(Modifier.fillMaxWidth())
        error?.let { Text(it,color=MaterialTheme.colors.error) }
        notice?.let { Text(it,color=Pine) }

        pending?.let { frozen ->
            Divider()
            Text("上次操作结果尚未确认",color=Pine)
            Text("请求尾号：${frozen.key.takeLast(10)}",color=Muted)
            TextButton(onClick={showPendingScope=!showPendingScope}) { Text(if(showPendingScope)"收起原作用域"else"查看原账号、账本与服务") }
            if(showPendingScope)Text("原账号：${frozen.scope.userId}\n原账本：${frozen.scope.ledgerId}\n原服务：${frozen.scope.origin}",color=Muted)
            when(frozen) {
                is ExpensePairConfirmSubmission -> Text("确认关联\n主记录：${frozen.input.primaryTransactionId}\n副记录：${frozen.input.secondaryTransactionId}\n原账本版本：${frozen.input.ledgerVersion}\n理由：${frozen.input.reason}")
                is ExpensePairWithdrawSubmission -> Text("撤回关联：${frozen.decisionId}\n原账本版本：${frozen.input.ledgerVersion} · 关联版本：${frozen.input.decisionVersion}\n理由：${frozen.input.reason}")
            }
            Text("已锁定原选择、版本、理由和请求标识。关闭再打开仍可恢复；重试只发送原请求，不生成新请求。",color=Muted)
            if(!pendingScopeMatches)Text("请返回原账号与账本核实，不能在当前作用域重试。",color=MaterialTheme.colors.error)
            if(!readOnly)Button(onClick={action { if(!latestReadOnly && expensePairUiPendingMatches(frozen,userId,ledgerId))submit(frozen) }},enabled=!busy&&pendingScopeMatches) { Text("使用原请求核实 / 重试") }
        }

        if(!readOnly && pending==null) {
            Divider()
            Text("从当前页选择两条未关联支出")
            Text("只列出当前搜索和分页中可见的未关联支出（最多50条）。不自动选择或猜测主记录；如不在本页，请先返回账本调整筛选。",color=Muted)
            Text("主记录：${primaryId ?: "未选择"}\n副记录：${secondaryId ?: "未选择"}")
            if(candidates.isEmpty())Text("当前页没有可选择的未关联支出。",color=Muted)
            candidates.forEach { item ->
                Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(3.dp)) {
                    Text("${item.date} · ${item.merchant} · ${expensePairUiMoney(item.amountMinor)} ${item.currency}")
                    Text("${item.id} · ${item.source} · ${item.account}",color=Muted)
                    Row(horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                        TextButton(onClick={primaryId=item.id;preview=null},enabled=canWrite&&item.id!=secondaryId) { Text(if(primaryId==item.id)"已选主记录"else"设为主记录") }
                        TextButton(onClick={secondaryId=item.id;preview=null},enabled=canWrite&&item.id!=primaryId) { Text(if(secondaryId==item.id)"已选副记录"else"设为副记录") }
                    }
                }
            }
            TextButton(onClick={primaryId=null;secondaryId=null;preview=null},enabled=canWrite) { Text("清空选择") }
            OutlinedTextField(reason,{reason=it;preview=null},label={Text("关联理由（必填）")},enabled=canWrite,modifier=Modifier.fillMaxWidth())
            val ticket=expensePairUiPreviewTicket(snapshot,primaryId,secondaryId,reason,readOnly,pending)
            OutlinedButton(onClick={
                val intent=ticket ?: return@OutlinedButton
                action {
                    val result=api.previewExpensePair(ledgerId,intent.input)
                    if(!latestReadOnly && intent.current(latestSnapshot,primaryId,secondaryId,reason) && pending==null)
                        preview=ExpensePairUiLoadedPreview(intent,result)
                }
            },enabled=canWrite&&ticket!=null) { Text("核验并预览影响") }
            visiblePreview?.let { checked ->
                val result=checked.value
                Text("保留主记录：${result.primaryId}\n副记录：${result.secondaryId}")
                Text("两条支出合计：${expensePairUiMoney(result.pairGrossBeforeMinor)} ${result.currency}\n关联后计入：${expensePairUiMoney(result.pairGrossAfterMinor)} ${result.currency}\n支出变化：${expensePairUiMoney(result.pairGrossDeltaMinor)} ${result.currency}")
                Text("这里只改变统计投影，不删除交易或原始凭证。账本版本、候选或理由变化后须重新预览。",color=Muted)
                Button(onClick={ action {
                    if(latestReadOnly || pending!=null || !checked.ticket.current(latestSnapshot,primaryId,secondaryId,reason))return@action
                    val registered=api.prepareExpensePairConfirm(ledgerId,checked.ticket.confirmInput(),::expensePairUiRequestKey)
                    pending=registered
                    if(registered.input!=checked.ticket.confirmInput() ||
                        !checked.ticket.current(latestSnapshot,primaryId,secondaryId,reason)) {
                        notice="已保留原请求；选择或版本已变化，请先核实原请求。"
                        return@action
                    }
                    submit(registered)
                } },enabled=canWrite) { Text("确认关联，仅计主记录一次") }
            }
        }

        Divider()
        Text("关联历史与两侧来源",color=Pine)
        if(dashboard?.ledgerId==ledgerId)dashboard.transactions.forEach { item ->
            TextButton(onClick={detailTarget=item.id;detailGeneration++},enabled=!busy) { Text("查看历史 · ${item.date} · ${item.merchant} · ${item.id}") }
        }
        detailTarget?.let { id ->
            Text("正在查看交易：$id",color=Muted)
            TextButton(onClick={detailGeneration++},enabled=!busy&&!detailLoading) { Text("刷新关联历史") }
        }
        if(detailTarget==null)Text("从当前页选择一条交易查看，或从账单详情进入。",color=Muted)
        if(detail!=null) {
            Text("当前账本版本：${detail.ledgerVersion}",color=Muted)
            if(detail.decisions.isEmpty())Text("这条交易没有关联历史。",color=Muted)
            detail.decisions.forEach { receipt ->
                Divider()
                Text("${if(receipt.status=="active")"关联中 · 支出仅计一次"else"已撤回 · 两条分别统计"}")
                Text("关联：${receipt.id} · 修订 ${receipt.version}\n主记录：${receipt.primaryId}\n副记录：${receipt.secondaryId}\n此修订写入时账本版本：${receipt.ledgerVersion}",color=Muted)
                val loaded=loadedDetail
                TextButton(onClick={
                    if(loaded!=null)action {
                        val sources=api.expensePairSources(ledgerId,receipt)
                        if(detailCurrent(loaded.ticket) && loadedDetail===loaded)
                            sourceResults=sourceResults+(receipt.id to ExpensePairUiSources(sources.primary,sources.secondary))
                    }
                },enabled=!busy) { Text("分别查看主 / 副记录来源") }
                sourceResults[receipt.id]?.let { sources ->
                    Text("主记录来源 · ${receipt.primaryId}",color=Pine)
                    if(sources.primary.isEmpty())Text("没有导入来源",color=Muted)
                    sources.primary.forEach { Text("${it.filename} · 第 ${it.line} 行") }
                    Text("副记录来源 · ${receipt.secondaryId}",color=Pine)
                    if(sources.secondary.isEmpty())Text("没有导入来源",color=Muted)
                    sources.secondary.forEach { Text("${it.filename} · 第 ${it.line} 行") }
                }
                if(!readOnly && receipt.status=="active" && pending==null) {
                    TextButton(onClick={withdrawDecisionId=receipt.id;withdrawReason="";withdrawAccepted=false},enabled=canWrite&&!detailLoading) { Text("准备撤回此关联") }
                    if(withdrawDecisionId==receipt.id) {
                        Text("撤回后原主、副记录都恢复独立统计；原始凭证保留。",color=Muted)
                        OutlinedTextField(withdrawReason,{withdrawReason=it;withdrawAccepted=false},label={Text("撤回理由（必填）")},enabled=canWrite,modifier=Modifier.fillMaxWidth())
                        Row {
                            Checkbox(checked=withdrawAccepted,onCheckedChange={withdrawAccepted=it},enabled=canWrite)
                            Text("确认恢复两条支出的独立统计",Modifier.padding(top=12.dp))
                        }
                        val permitted=expensePairUiCanWithdraw(detail,dashboardVersion,receipt,readOnly,busy,pending,withdrawReason,withdrawAccepted)
                        Button(onClick={ action {
                            val fresh=loadedDetail?.takeIf { detailCurrent(it.ticket) }?.value
                            if(!expensePairUiCanWithdraw(fresh,latestVersion,receipt,latestReadOnly,false,pending,withdrawReason,withdrawAccepted))return@action
                            val intended=ExpensePairWithdrawInput(fresh!!.ledgerVersion,receipt.version,withdrawReason.trim())
                            val registered=api.prepareExpensePairWithdraw(ledgerId,receipt.id,intended,::expensePairUiRequestKey)
                            pending=registered
                            if(registered.decisionId!=receipt.id || registered.input!=intended ||
                                loadedDetail?.value!==fresh ||
                                !expensePairUiCanWithdraw(fresh,latestVersion,receipt,latestReadOnly,false,null,withdrawReason,withdrawAccepted)) {
                                notice="已保留原撤回请求；详情或版本已变化，请先核实原请求。"
                                return@action
                            }
                            submit(registered)
                        } },enabled=permitted&&!detailLoading) { Text("确认撤回关联") }
                    }
                }
            }
        }
        TextButton(onClick=onClose) { Text(if(pending!=null)"关闭（保留待核实请求）"else"返回账本") }
    }
}
