package com.letrahoo.monee

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

internal fun String.ledgerRole()=when(this){"owner"->"所有者";"editor"->"可编辑";else->"只读"}

@Composable
internal fun WorkspaceScreen(api:LedgerApi,user:AuthUser,authBusy:Boolean,authError:String?,onLogout:()->Unit,onManage:()->Unit,onAccessLost:()->Unit){
 val scope=rememberCoroutineScope()
 var ledgers by remember {mutableStateOf<List<LedgerInfo>>(emptyList())}
 var invites by remember {mutableStateOf<List<LedgerInvitation>>(emptyList())}
 var account by remember {mutableStateOf<AccountState?>(null)}
 var initialSelectionDone by remember {mutableStateOf(false)}
 var selected by remember {mutableStateOf<String?>(null)}
 var showAccount by remember {mutableStateOf(false)}
 var manage by remember {mutableStateOf<LedgerInfo?>(null)}
 var members by remember {mutableStateOf<List<LedgerMember>>(emptyList())}
 var busy by remember {mutableStateOf(false)}
 var error by remember {mutableStateOf<String?>(null)}
 var notice by remember {mutableStateOf<String?>(null)}
 var name by remember {mutableStateOf("")}
 var displayName by remember {mutableStateOf(user.displayName)}
 var inviteUser by remember {mutableStateOf("")}
 var inviteRole by remember {mutableStateOf("viewer")}
 var mergeConfirm by remember {mutableStateOf<String?>(null)}
 var unlinkConfirm by remember {mutableStateOf<LinkedIdentity?>(null)}
 var refresh by remember {mutableStateOf(0)}
 LaunchedEffect(refresh,busy){
  if(busy)return@LaunchedEffect
  while(true){
   try{
    val list=api.ledgers().ledgers
    ledgers=list
    if(!initialSelectionDone){
     if(list.size==1)selected=list.single().id
     initialSelectionDone=true
    }
    if(selected!=null&&list.none{it.id==selected})selected=null
    invites=api.invitations().invitations
    account=api.account();error=null
   }catch(e:CancellationException){throw e}catch(e:Exception){selected=null;if(e is LedgerException&&e.accessLost)onAccessLost()else error=e.message}
   delay(5000)
  }
 }
 fun act(action:suspend()->Unit){scope.launch{busy=true;error=null;notice=null;try{action();refresh++}catch(e:CancellationException){throw e}catch(e:Exception){if(e is LedgerException&&e.accessLost)onAccessLost()else error=e.message}finally{busy=false}}}
 val active=ledgers.find{it.id==selected}
 if(active!=null){
  key(active.id,active.role){LedgerScreen(api,user,active,onWorkspace={selected=null;showAccount=false},onAccount={selected=null;showAccount=true},authBusy=authBusy,authError=authError,onLogout=onLogout,onManage=onManage,onAccessLost=onAccessLost)}
  return
 }
 Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(28.dp),verticalArrangement=Arrangement.spacedBy(16.dp)){
  BrandHeader()
  Text(if(showAccount)"账号设置"else"我的账本",fontSize=28.sp,color=Pine)
  Row(horizontalArrangement=Arrangement.spacedBy(10.dp)){
   TextButton(onClick={showAccount=!showAccount},enabled=!busy){Text(if(showAccount)"我的账本"else"账号设置")}
   if(user.role=="superadmin")TextButton(onClick=onManage,enabled=!busy){Text("系统准入")}
   TextButton(onClick=onLogout,enabled=!busy&&!authBusy){Text("退出登录")}
  }
  if(busy)LinearProgressIndicator(Modifier.fillMaxWidth())
  (error?:authError)?.let{Text(it,color=MaterialTheme.colors.error)}
  notice?.let{Text(it,color=Pine)}
  if(showAccount){
   Text("${account?.user?.displayName?:user.displayName}",fontSize=20.sp)
   Text("Monee 账号 ID（用于账本邀请）",fontSize=12.sp,color=Muted)
   SelectionContainer{Text(user.id)}
   OutlinedTextField(displayName,{displayName=it},label={Text("昵称")},singleLine=true)
   TextButton(onClick={act{api.rename(displayName);notice="昵称已保存"}},enabled=!busy){Text("保存昵称")}
   account?.identities?.forEach{i->
    Row(horizontalArrangement=Arrangement.spacedBy(12.dp)){
     Text("${i.provider.displayProvider()} · ${i.username.ifBlank{i.email.ifBlank{i.subject}}}")
     TextButton(onClick={unlinkConfirm=i},enabled=!busy&&(account?.identities?.size?:0)>1){Text("解绑")}
    }
   }
   unlinkConfirm?.let{i->
    Text("确认解绑 ${i.provider.displayProvider()}？该渠道的登录会话将失效。")
    Row{TextButton(onClick={act{api.unlink(i);unlinkConfirm=null}},enabled=!busy){Text("确认解绑")};TextButton(onClick={unlinkConfirm=null}){Text("取消")}}
   }
   Row(horizontalArrangement=Arrangement.spacedBy(10.dp)){
    listOf("github","google").filter{provider->account!=null&&account!!.identities.none{it.provider==provider}}.forEach{provider->OutlinedButton(onClick={act{api.login(provider,"link");notice="请完成身份验证，再返回查看绑定结果"}},enabled=!busy){Text("绑定 ${provider.displayProvider()}")}}
   }
   account?.pendingMerges?.forEach{merge->
    Text("${merge.provider.displayProvider()} · ${merge.username.ifBlank{merge.name}} 已注册为另一个账号。")
    TextButton(onClick={mergeConfirm=merge.id},enabled=!busy){Text("查看合并确认")}
    if(mergeConfirm==merge.id){
     Text("合并后保留双方账本，同账本取较高权限。另一个账号的旧会话失效，历史操作记录保留。")
     Button(onClick={act{api.merge(merge.id);mergeConfirm=null;notice="账号已合并"}},enabled=!busy){Text("确认合并到当前账号")}
    }
   }
   Divider()
  }
  if(!showAccount){
  invites.forEach{i->
   Text("${i.invitedBy} 邀请你加入「${i.ledgerName}」· ${i.role.ledgerRole()}")
   Row{Button(onClick={act{api.respond(i.id,true)}},enabled=!busy){Text("接受")};TextButton(onClick={act{api.respond(i.id,false)}},enabled=!busy){Text("拒绝")}}
  }
  if(ledgers.isEmpty()&&error==null)Text("还没有账本，创建一个或接受他人的邀请。",color=Muted)
  ledgers.forEach{l->
   Row(horizontalArrangement=Arrangement.spacedBy(12.dp)){
    OutlinedButton(onClick={selected=l.id},enabled=!busy){Text("${l.name} · ${l.role.ledgerRole()}")}
    if(l.role=="owner")TextButton(onClick={act{manage=l;members=api.members(l.id).members}},enabled=!busy){Text("管理成员")}
   }
  }
  OutlinedTextField(name,{name=it},label={Text("新账本名称")},singleLine=true)
  Button(onClick={act{val l=api.newLedger(name);ledgers=api.ledgers().ledgers;selected=l.id;name=""}},enabled=!busy&&name.isNotBlank()){Text("创建账本")}
  }
  manage?.takeIf{!showAccount}?.let{l->
   Divider();Text("${l.name} · 成员",fontSize=20.sp)
   members.forEach{m->
    Text("${m.name} · ${m.role.ledgerRole()}")
    if(m.role!="owner")Row{
     TextButton(onClick={act{api.changeMember(l.id,m,if(m.role=="viewer")"editor"else"viewer");members=api.members(l.id).members}},enabled=!busy){Text(if(m.role=="viewer")"设为可编辑"else"设为只读")}
     TextButton(onClick={act{api.changeMember(l.id,m,"remove");members=api.members(l.id).members}},enabled=!busy){Text("移除成员")}
    }
   }
   OutlinedTextField(inviteUser,{inviteUser=it},label={Text("对方的 Monee 账号 ID")},singleLine=true)
   Row{TextButton(onClick={inviteRole="viewer"}){Text(if(inviteRole=="viewer")"✓ 只读"else"只读")};TextButton(onClick={inviteRole="editor"}){Text(if(inviteRole=="editor")"✓ 可编辑"else"可编辑")}}
   Button(onClick={act{api.invite(l.id,inviteUser.trim(),inviteRole);inviteUser="";notice="邀请已创建，等待对方接受"}},enabled=!busy&&inviteUser.isNotBlank()){Text("邀请加入")}
   TextButton(onClick={manage=null}){Text("收起成员管理")}
  }
 }
}
