package com.letrahoo.monee

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
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
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.data.*
import com.letrahoo.monee.resources.Res
import com.letrahoo.monee.resources.logo
import com.letrahoo.monee.resources.noto_sans_sc
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.Font
import org.jetbrains.compose.resources.painterResource

@Composable
fun App() {
    val api=remember {LedgerApi()}
    val scope=rememberCoroutineScope()
    var auth by remember {mutableStateOf<AuthState?>(null)}
    var connectionError by remember {mutableStateOf<String?>(null)}
    var actionError by remember {mutableStateOf<String?>(null)}
    var refresh by remember {mutableStateOf(0)}
    var busy by remember {mutableStateOf(false)}
    var managing by remember {mutableStateOf(false)}
    var loginJob by remember {mutableStateOf<Job?>(null)}
    DisposableEffect(api) {onDispose {api.close()}}
    LaunchedEffect(refresh) {
        while(true) {
            try {auth=api.authState();connectionError=null}
            catch(e:CancellationException){throw e}
            catch(e:Exception){connectionError=e.message ?: "无法验证登录状态"}
            delay(5000)
        }
    }
    fun accessLost(){auth=null;managing=false;refresh++}
    fun logout(){scope.launch {
        busy=true;actionError=null
        try {api.logout();auth=null;managing=false;refresh++}
        catch(e:CancellationException){throw e}
        catch(e:Exception){actionError=e.message}
        finally{busy=false}
    }}
    fun login(provider:String){loginJob=scope.launch {
        busy=true;actionError=null
        try {
            if(auth?.user!=null)api.logout()
            auth=null;managing=false
            api.login(provider);refresh++
        }catch(e:TimeoutCancellationException){actionError="登录等待已超时，请重新发起"}
        catch(e:CancellationException){throw e}
        catch(e:Exception){actionError=e.message ?: "登录失败，请重试"}
        finally{busy=false}
    }}
    val user=auth?.user
    MaterialTheme(colors=lightColors(primary=Pine,background=Color(0xFFF6F7F2),onBackground=Ink,onSurface=Ink),
        typography=Typography(defaultFontFamily=FontFamily(Font(Res.font.noto_sans_sc)))) {
        if(user?.allowed==true&&connectionError==null) {
            key(user.provider,user.subject) {
                if(managing&&user.role=="superadmin") AccessManagement(api,user,onBack={managing=false},onAccessLost=::accessLost)
                else LedgerScreen(api,user,busy,actionError,onLogout=::logout,onManage={managing=true},onAccessLost=::accessLost)
            }
        } else {
            Column(Modifier.fillMaxSize().background(MaterialTheme.colors.background).verticalScroll(rememberScrollState()).padding(24.dp),
                horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(20.dp)) {
                Spacer(Modifier.height(24.dp))
                Image(painterResource(Res.drawable.logo),"Monee Logo",Modifier.size(80.dp))
                Text("Monee",fontSize=32.sp,fontWeight=FontWeight.Bold,color=Pine)
                Text("Know Your Money. Own Your Future.",color=Muted,fontSize=13.sp)
                Surface(shape=RoundedCornerShape(20.dp),modifier=Modifier.widthIn(max=540.dp).fillMaxWidth()) {
                    Column(Modifier.padding(26.dp),verticalArrangement=Arrangement.spacedBy(16.dp)) {
                        Text(if(user!=null&&!user.allowed)"无数据访问权限"else"登录你的 Monee",fontSize=25.sp,fontWeight=FontWeight.Bold,color=Pine)
                        if(user!=null&&!user.allowed) {
                            Text("${user.provider.displayProvider()} · ${user.label}")
                            Text("你的账号还未获得本地账本的访问权限，请联系超管添加白名单。",color=Muted)
                            Text("账号 ID：${user.subject}",fontSize=12.sp,color=Muted)
                        }else Text("使用 Google 或 GitHub 登录。只有白名单内的账号可以查看和管理这份本地账本。",color=Muted)
                        if(auth==null&&connectionError==null&&!busy)LinearProgressIndicator(Modifier.fillMaxWidth())
                        connectionError?.let{Text(it,color=MaterialTheme.colors.error)}
                        actionError?.let{Text(it,color=MaterialTheme.colors.error)}
                        if(busy){LinearProgressIndicator(Modifier.fillMaxWidth());Text("请在浏览器中完成登录，完成后应用会自动更新。",fontSize=13.sp)}
                        listOf("google","github").forEach {provider->
                            val enabled=auth?.providers?.any{it.id==provider&&it.enabled}==true
                            OutlinedButton(onClick={login(provider)},enabled=enabled&&!busy&&connectionError==null,modifier=Modifier.fillMaxWidth()){
                                Text("使用 ${provider.displayProvider()} 登录${if(!enabled)" · 尚未配置"else""}")
                            }
                        }
                        if(auth!=null&&auth!!.providers.none{it.enabled})Text("登录服务尚未配置。请按项目中的登录配置指南填写本机 OAuth 应用信息，再启动服务。",fontSize=12.sp,color=Muted)
                        Row(horizontalArrangement=Arrangement.spacedBy(8.dp)){
                            TextButton(onClick={api.reconnect();refresh++},enabled=!busy){Text("刷新登录状态")}
                            if(user!=null)TextButton(onClick=::logout,enabled=!busy){Text("退出登录")}
                            if(busy&&loginJob!=null)TextButton(onClick={loginJob?.cancel();loginJob=null;busy=false;refresh++}){Text("取消等待")}
                        }
                    }
                }
                Text("账本保存在本机。登录仅用于验证身份与访问权限。",fontSize=12.sp,color=Muted)
            }
        }
    }
}

internal fun String.displayProvider()=if(this=="google")"Google"else"GitHub"

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AccessManagement(api:LedgerApi,user:AuthUser,onBack:()->Unit,onAccessLost:()->Unit){
    val scope=rememberCoroutineScope()
    var entries by remember {mutableStateOf<List<AccessEntry>>(emptyList())}
    var history by remember {mutableStateOf<List<AccessAudit>>(emptyList())}
    var input by remember {mutableStateOf(AccessSelector())}
    var error by remember {mutableStateOf<String?>(null)}
    var notice by remember {mutableStateOf<String?>(null)}
    var busy by remember {mutableStateOf(false)}
    var loaded by remember {mutableStateOf(false)}
    suspend fun reload(){entries=api.accessList().entries;history=api.accessHistory().events;loaded=true}
    fun action(block:suspend ()->Unit){scope.launch{
        busy=true;error=null;notice=null
        try{block()}
        catch(e:CancellationException){throw e}
        catch(e:Exception){if(e is LedgerException&&e.accessLost)onAccessLost()else error=e.message}
        finally{busy=false}
    }}
    LaunchedEffect(Unit){
        busy=true
        try{reload()}catch(e:CancellationException){throw e}catch(e:Exception){if(e is LedgerException&&e.accessLost)onAccessLost()else error=e.message}finally{busy=false}
    }
    Column(Modifier.fillMaxSize().background(MaterialTheme.colors.background).verticalScroll(rememberScrollState()).padding(24.dp),verticalArrangement=Arrangement.spacedBy(18.dp)){
        FlowRow(horizontalArrangement=Arrangement.spacedBy(16.dp),verticalArrangement=Arrangement.spacedBy(8.dp)){
            Text("访问白名单",fontSize=28.sp,fontWeight=FontWeight.Bold,color=Pine)
            TextButton(onClick=onBack,enabled=!busy){Text("返回账本")}
            TextButton(onClick={action{reload()}},enabled=!busy){Text("刷新名单")}
        }
        Text("超管：${user.label}",color=Muted)
        Text("白名单成员可以读写这份本地账本。停用后，账号的下一次数据请求将被拒绝。初始超管受保护。",color=Muted,fontSize=13.sp)
        if(busy)LinearProgressIndicator(Modifier.fillMaxWidth())
        error?.let{Text(it,color=MaterialTheme.colors.error)}
        notice?.let{Text(it,color=Pine)}
        Surface(shape=RoundedCornerShape(18.dp)){
            Column(Modifier.fillMaxWidth().padding(22.dp),verticalArrangement=Arrangement.spacedBy(12.dp)){
                Text("添加账号",fontSize=21.sp,fontWeight=FontWeight.Bold)
                FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)){
                    listOf("github","google").forEach{provider->
                        OutlinedButton(onClick={input=AccessSelector(provider,if(provider=="github")"username"else"email")},enabled=!busy){Text("${if(input.provider==provider)"✓ "else""}${provider.displayProvider()}")}
                    }
                }
                FlowRow(horizontalArrangement=Arrangement.spacedBy(8.dp)){
                    val kinds=if(input.provider=="github")listOf("username","subject")else listOf("email","subject")
                    kinds.forEach{kind->OutlinedButton(onClick={input=input.copy(kind=kind,value="")},enabled=!busy){Text("${if(input.kind==kind)"✓ "else""}${kind.selectorLabel()}")}}
                }
                OutlinedTextField(input.value,{input=input.copy(value=it)},Modifier.fillMaxWidth(),label={Text(input.kind.selectorLabel())},singleLine=true,enabled=!busy)
                OutlinedTextField(input.note,{input=input.copy(note=it)},Modifier.fillMaxWidth(),label={Text("备注（可选）")},singleLine=true,enabled=!busy)
                Text(if(input.kind=="subject")"按平台验证过的稳定账号 ID 匹配，请核对 ID。"else if(input.provider=="github")"用户名会经 GitHub 查询并绑定到稳定账号 ID。"else"邮箱邀请在首次验证登录时绑定账号 ID，仅支持 Google 已验证的 Gmail / Workspace 邮箱；其他 Google 账号请填写账号 ID。",fontSize=12.sp,color=Muted)
                Button(onClick={action{api.addAccess(input);input=input.copy(value="",note="");reload();notice="账号已添加到白名单。"}},enabled=!busy&&input.value.isNotBlank()){Text("添加到白名单")}
            }
        }
        Text("已配置账号 · ${entries.size}",fontSize=20.sp,fontWeight=FontWeight.Bold)
        if(loaded&&entries.isEmpty())Text("尚无白名单账号。",color=Muted)
        entries.forEach{entry->
            Surface(shape=RoundedCornerShape(14.dp)){
                Column(Modifier.fillMaxWidth().padding(20.dp),verticalArrangement=Arrangement.spacedBy(8.dp)){
                    Text("${entry.provider.displayProvider()} · ${entry.value}",fontWeight=FontWeight.Bold)
                    Text(if(entry.role=="superadmin")"超管 · 受保护"else if(entry.enabled)"已启用"else"已停用",color=if(entry.enabled)Pine else Muted)
                    Text(if(entry.subject.isBlank())"等待首次验证登录后绑定账号 ID"else"账号 ID：${entry.subject}",fontSize=12.sp,color=Muted)
                    if(entry.note.isNotBlank())Text(entry.note,fontSize=13.sp)
                    if(!entry.protected)OutlinedButton(onClick={action{api.setAccess(entry);reload();notice=if(entry.enabled)"账号已停用。"else"账号已启用。"}},enabled=!busy){Text(if(entry.enabled)"停用访问"else"启用访问")}
                }
            }
        }
        if(history.isNotEmpty()){
            Text("最近的权限变更",fontSize=20.sp,fontWeight=FontWeight.Bold)
            history.take(12).forEach{event->Text("${event.createdAt.take(19).replace('T',' ')} UTC · ${event.action.auditLabel()} · ${event.actor}",fontSize=12.sp,color=Muted)}
        }
    }
}
private fun String.selectorLabel()=when(this){"username"->"GitHub 用户名";"email"->"Google 邮箱";else->"稳定账号 ID"}
private fun String.auditLabel()=when(this){"add"->"添加";"bind"->"绑定身份";"enable"->"启用";"disable"->"停用";else->this}
