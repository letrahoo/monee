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
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.Font
import org.jetbrains.compose.resources.painterResource

@Composable
fun App() {
    var prepared by remember { mutableStateOf(false) }
    var preparationFailed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        try {
            preparePlatformClient()
            prepared = true
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            preparationFailed = true
        }
    }
    if (!prepared) {
        MaterialTheme {
            Column(Modifier.fillMaxSize().background(Color(0xFFF6F7F2)).padding(24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center) {
                if (!preparationFailed) CircularProgressIndicator(color = Pine)
                Spacer(Modifier.height(16.dp))
                Text(if (preparationFailed) "无法准备连接，请重新启动应用。" else "正在准备连接…", color = Ink)
            }
        }
        return
    }
    PreparedApp()
}

@Composable
private fun PreparedApp() {
    val api=remember {LedgerApi()}
    val scope=rememberCoroutineScope()
    var auth by remember {mutableStateOf<AuthState?>(null)}
    var connectionError by remember {mutableStateOf<String?>(null)}
    var actionError by remember {mutableStateOf<String?>(null)}
    var refresh by remember {mutableStateOf(0)}
    var busy by remember {mutableStateOf(false)}
    var loggingOut by remember {mutableStateOf(false)}
    var showPrivacy by remember {mutableStateOf(false)}
    var showIdentity by remember {mutableStateOf(false)}
    var managing by remember {mutableStateOf(false)}
    var loginJob by remember {mutableStateOf<Job?>(null)}
    DisposableEffect(api) {onDispose {api.close()}}
    LaunchedEffect(refresh,busy) {
        if(busy)return@LaunchedEffect
        val revision=refresh
        while(true) {
            try {
                val state=api.authState()
                ensureActive()
                if(!busy&&refresh==revision){auth=state;connectionError=null}
            }
            catch(e:CancellationException){throw e}
            catch(e:Exception){if(!busy&&refresh==revision)connectionError=e.message ?: "无法验证登录状态"}
            delay(5000)
        }
    }
    fun accessLost(){auth=null;managing=false;refresh++}
    fun logout(){scope.launch {
        if(busy)return@launch
        busy=true;loggingOut=true;actionError=null
        loginJob?.cancel();loginJob=null
        try {api.logout();auth=null;managing=false;connectionError=null;refresh++}
        catch(e:CancellationException){throw e}
        catch(e:Exception){actionError=e.message ?: "退出登录失败，请重试"}
        finally{loggingOut=false;busy=false}
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
            key(user.id) {
                if(managing&&user.role=="superadmin") AccessManagement(api,user,busy,actionError,onBack={managing=false},onLogout=::logout,onAccessLost=::accessLost)
                else WorkspaceScreen(api,user,busy,actionError,onLogout=::logout,onManage={managing=true},onAccessLost=::accessLost)
            }
        } else {
            Column(Modifier.fillMaxSize().background(MaterialTheme.colors.background).verticalScroll(rememberScrollState()).padding(24.dp),
                horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(20.dp)) {
                Spacer(Modifier.height(40.dp))
                Image(painterResource(Res.drawable.logo),"Monee Logo",Modifier.size(64.dp))
                Text("monee",fontSize=30.sp,fontWeight=FontWeight.Bold,color=Pine)
                Text("Know Your Money. Own Your Future.",color=Muted,fontSize=13.sp)
                Surface(shape=RoundedCornerShape(24.dp),modifier=Modifier.widthIn(max=440.dp).fillMaxWidth()) {
                    Column(Modifier.padding(32.dp),verticalArrangement=Arrangement.spacedBy(18.dp)) {
                        Text(if(user!=null&&!user.allowed)"无数据访问权限"else"登录你的账本",fontSize=22.sp,fontWeight=FontWeight.Bold,color=Pine)
                        if(user!=null&&!user.allowed) {
                            Text("${user.provider.displayProvider()} · ${user.label}")
                            Text("请联系管理员开通访问权限。",color=Muted)
                            TextButton(onClick={showIdentity=!showIdentity}){Text("账号信息")}
                            if(showIdentity) Text("账号 ID：${user.subject}",fontSize=12.sp,color=Muted)
                        }
                        if(auth==null&&connectionError==null&&!busy)LinearProgressIndicator(Modifier.fillMaxWidth())
                        connectionError?.let{Text(it,color=MaterialTheme.colors.error)}
                        actionError?.let{Text(it,color=MaterialTheme.colors.error)}
                        if(busy){LinearProgressIndicator(Modifier.fillMaxWidth());Text(if(loggingOut)"正在退出登录…"else"请在浏览器中完成登录。",fontSize=13.sp)}
                        listOf("google","github").forEach {provider->
                            val enabled=auth?.providers?.any{it.id==provider&&it.enabled}==true
                            OutlinedButton(onClick={login(provider)},enabled=enabled&&!busy&&connectionError==null,modifier=Modifier.fillMaxWidth().heightIn(min=52.dp),shape=RoundedCornerShape(12.dp)){
                                Text("使用 ${provider.displayProvider()} 登录${if(auth!=null&&!enabled)" · 暂不可用"else""}")
                            }
                        }
                        if(auth!=null&&auth!!.providers.none{it.enabled})Text("登录暂不可用，请联系管理员。",fontSize=12.sp,color=Muted)
                        Row(horizontalArrangement=Arrangement.spacedBy(8.dp)){
                            if(connectionError!=null) TextButton(onClick={api.reconnect();refresh++},enabled=!busy){Text("重试")}
                            if(user!=null)TextButton(onClick=::logout,enabled=!busy){Text("退出登录")}
                            if(busy&&loginJob!=null)TextButton(onClick={loginJob?.cancel();loginJob=null;busy=false;refresh++}){Text("取消等待")}
                        }
                    }
                }
                TextButton(onClick={showPrivacy=!showPrivacy}){Text("数据与隐私")}
                if(showPrivacy) Text("账本保存在这台设备，尚未云端同步。只有获授权的成员可以访问对应账本。",fontSize=12.sp,color=Muted)
            }
        }
    }
}

internal fun String.displayProvider()=when(this){"google"->"Google";"github"->"GitHub";"feishu"->"飞书";else->this}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AccessManagement(api:LedgerApi,user:AuthUser,authBusy:Boolean,authError:String?,onBack:()->Unit,onLogout:()->Unit,onAccessLost:()->Unit){
    val scope=rememberCoroutineScope()
    var entries by remember {mutableStateOf<List<AccessEntry>>(emptyList())}
    var registered by remember {mutableStateOf<List<RegisteredUser>>(emptyList())}
    var history by remember {mutableStateOf<List<AccessAudit>>(emptyList())}
    var input by remember {mutableStateOf(AccessSelector())}
    var error by remember {mutableStateOf<String?>(null)}
    var notice by remember {mutableStateOf<String?>(null)}
    var busy by remember {mutableStateOf(false)}
    var loaded by remember {mutableStateOf(false)}
    var showHistory by remember {mutableStateOf(false)}
    var expandedEntry by remember {mutableStateOf<String?>(null)}
    suspend fun reload(){registered=api.registeredUsers().users;entries=api.accessList().entries;history=api.accessHistory().events;loaded=true}
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
    SettingsPage{
        BrandHeader()
        FlowRow(horizontalArrangement=Arrangement.spacedBy(16.dp),verticalArrangement=Arrangement.spacedBy(8.dp)){
            Text("访问白名单",fontSize=28.sp,fontWeight=FontWeight.Bold,color=Pine)
            TextButton(onClick=onBack,enabled=!busy&&!authBusy){Text("返回账本")}
            TextButton(onClick={action{reload()}},enabled=!busy&&!authBusy){Text("刷新名单")}
            OutlinedButton(onClick=onLogout,enabled=!busy&&!authBusy){Text(if(authBusy)"正在退出…"else"退出登录")}
        }
        Text(user.label,color=Muted)
        Text("获准后可以创建账本；访问他人账本还需单独授权。停用统一账号会影响其全部登录渠道。",color=Muted,fontSize=13.sp)
        if(busy||authBusy)LinearProgressIndicator(Modifier.fillMaxWidth())
        authError?.let{Text(it,color=MaterialTheme.colors.error)}
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
                if(input.provider=="google"&&input.kind=="email") Text("支持 Gmail 或 Google Workspace 邮箱；其他邮箱请选择账号 ID。",fontSize=12.sp,color=Muted)
                Button(onClick={action{api.addAccess(input);input=input.copy(value="",note="");reload();notice="账号已添加到白名单。"}},enabled=!busy&&!authBusy&&input.value.isNotBlank()){Text("添加到白名单")}
            }
        }
        Text("已注册用户 · ${registered.size}",fontSize=20.sp,fontWeight=FontWeight.Bold)
        registered.forEach{person->
            Column(verticalArrangement=Arrangement.spacedBy(6.dp)){
                Text("${person.name} · ${if(person.enabled)"已获准"else"待开通/已停用"}")
                androidx.compose.foundation.text.selection.SelectionContainer{Text(person.id,fontSize=12.sp,color=Muted)}
                if(person.role!="superadmin")OutlinedButton(onClick={action{api.setRegisteredUser(person);reload()}},enabled=!busy){Text(if(person.enabled)"停用账号"else"开通账号")}
            }
        }
        Text("预设准入身份 · ${entries.size}",fontSize=20.sp,fontWeight=FontWeight.Bold)
        if(loaded&&entries.isEmpty())Text("尚无白名单账号。",color=Muted)
        entries.forEach{entry->
            Surface(shape=RoundedCornerShape(14.dp)){
                Column(Modifier.fillMaxWidth().padding(20.dp),verticalArrangement=Arrangement.spacedBy(8.dp)){
                    Text("${entry.provider.displayProvider()} · ${entry.value}",fontWeight=FontWeight.Bold)
                    Text(if(entry.role=="superadmin")"超管"else if(entry.enabled)"已启用"else"已停用",color=if(entry.enabled)Pine else Muted)
                    if(entry.subject.isBlank())Text("等待首次登录",fontSize=12.sp,color=Muted)
                    TextButton(onClick={expandedEntry=if(expandedEntry==entry.id)null else entry.id}){Text(if(expandedEntry==entry.id)"收起"else"详情")}
                    if(expandedEntry==entry.id){
                        if(entry.subject.isNotBlank())Text("账号 ID：${entry.subject}",fontSize=12.sp,color=Muted)
                        if(entry.note.isNotBlank())Text(entry.note,fontSize=13.sp)
                    }
                    if(!entry.protected&&entry.subject.isBlank())OutlinedButton(onClick={action{api.setAccess(entry);reload();notice=if(entry.enabled)"账号已停用。"else"账号已启用。"}},enabled=!busy&&!authBusy){Text(if(entry.enabled)"停用访问"else"启用访问")}
                }
            }
        }
        if(history.isNotEmpty()){
            TextButton(onClick={showHistory=!showHistory}){Text(if(showHistory)"收起操作记录"else"操作记录")}
            if(showHistory) history.take(12).forEach{event->
                val target=entries.find{it.id==event.entryId}
                val actor=if(event.actor=="bootstrap")"系统"else if(event.actor=="${user.provider}:${user.subject}")user.label else entries.find{"${it.provider}:${it.subject}"==event.actor}?.value ?: "管理员"
                Column(verticalArrangement=Arrangement.spacedBy(4.dp)){
                    Text("${event.action.auditLabel()} · ${target?.provider?.displayProvider().orEmpty()} ${target?.value ?: "账号"}",fontSize=13.sp)
                    Text("${event.createdAt.take(19).replace('T',' ')} UTC · $actor",fontSize=12.sp,color=Muted)
                }
            }
        }
    }
}
private fun String.selectorLabel()=when(this){"username"->"GitHub 用户名";"email"->"Google 邮箱";else->"账号 ID"}
private fun String.auditLabel()=when(this){"add"->"添加";"bind"->"绑定身份";"enable"->"启用";"disable"->"停用";"set_account_access"->"调整账号准入";"merge_account"->"合并账号";"unlink_identity"->"解绑身份";else->this}
