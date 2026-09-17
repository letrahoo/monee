package com.letrahoo.monee.data

import java.nio.file.Path
import java.security.MessageDigest
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

// Credentials never enter command arguments, files, logs, or build resources.
// The account binds the credential to this data directory and service origin.
internal object SessionVault {
    private const val service = "Monee Desktop Session"
    internal fun account(baseUrl:String,dataDirectory:String):String = MessageDigest.getInstance("SHA-256")
        .digest((Path.of(dataDirectory).toAbsolutePath().normalize().toString()+"\n"+baseUrl).toByteArray())
        .joinToString(""){"%02x".format(it)}
    internal fun valid(token:String)=token.matches(Regex("[A-Za-z0-9_-]{32,256}"))
    private fun supported()=System.getProperty("os.name").startsWith("Mac")
    private fun read(account:String):String? {
        val p=ProcessBuilder("/usr/bin/security","find-generic-password","-s",service,"-a",account,"-w")
            .redirectError(ProcessBuilder.Redirect.DISCARD).start()
        if(!p.waitFor(15,TimeUnit.SECONDS)){p.destroyForcibly();return null}
        if(p.exitValue()!=0)return null
        return p.inputStream.bufferedReader().use{it.readText().trim()}.takeIf(::valid)
    }
    suspend fun load(base:String):String?=withContext(Dispatchers.IO){
        if(!supported())return@withContext null
        runCatching {read(account(base,desktopDataDirectory().toString()))}.getOrNull()
    }
    suspend fun save(base:String,token:String)=withContext(Dispatchers.IO){
        if(!supported())return@withContext
        require(valid(token)) {"Invalid session credential"}
        val account=account(base,desktopDataDirectory().toString())
        // security's interactive mode reads the command from a private pipe;
        // only fixed service/account and a validated base64url token are used.
        val p=ProcessBuilder("/usr/bin/security","-i")
            .redirectOutput(ProcessBuilder.Redirect.DISCARD).redirectError(ProcessBuilder.Redirect.DISCARD).start()
        p.outputStream.bufferedWriter().use {it.write("add-generic-password -U -s \"$service\" -a $account -w $token\nquit\n")}
        if(!p.waitFor(15,TimeUnit.SECONDS)){p.destroyForcibly();throw LedgerException("钥匙串保存超时，请允许 Monee 使用钥匙串后重试。")}
        if(read(account)!=token)throw LedgerException("未能保存登录态，请允许 Monee 使用钥匙串。")
    }
    suspend fun clear(base:String)=withContext(Dispatchers.IO){
        if(!supported())return@withContext
        val p=ProcessBuilder("/usr/bin/security","delete-generic-password","-s",service,"-a",account(base,desktopDataDirectory().toString()))
            .redirectOutput(ProcessBuilder.Redirect.DISCARD).redirectError(ProcessBuilder.Redirect.DISCARD).start()
        if(!p.waitFor(15,TimeUnit.SECONDS)){p.destroyForcibly();throw LedgerException("钥匙串清理超时，请重试退出登录。")}
        // 44 = item not found. Removing an absent credential is idempotent.
        if(p.exitValue()!=0&&p.exitValue()!=44)throw LedgerException("未能清除钥匙串登录态，请重试。")
    }
}
internal actual suspend fun loadSession(base:String)=SessionVault.load(base)
internal actual suspend fun saveSession(base:String,token:String)=SessionVault.save(base,token)
internal actual suspend fun clearSession(base:String)=SessionVault.clear(base)
