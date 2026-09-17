@file:OptIn(kotlin.js.ExperimentalWasmJsInterop::class)

package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.js.Js
import io.ktor.client.engine.js.JsError
import io.ktor.client.plugins.HttpTimeout
import kotlinx.coroutines.await
import kotlin.js.JsString
import kotlin.js.Promise
import kotlin.js.js

internal actual fun platformClient() = HttpClient(Js) {
    install(HttpTimeout) { requestTimeoutMillis = 20_000 }
}
internal actual fun isPlatformNetworkFailure(cause: Throwable): Boolean =
    cause is JsError || cause.cause is JsError
private fun pageOrigin(): String = js("window.location.origin")
internal actual suspend fun discoverConnection(client: HttpClient): Connection =
    Connection(pageOrigin())

private fun selectCSV(native: Boolean, wechat: Boolean): Promise<JsString> = js("""
new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file'; input.accept = wechat ? '.csv,text/csv,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' : '.csv,text/csv'; input.style.display = 'none';
    document.body.appendChild(input);
    const finish = value => { input.remove(); resolve(JSON.stringify(value)); };
    input.oncancel = () => finish({name:'', text:'',contentBase64:''});
    input.onchange = async () => {
        try {
            const file = input.files[0];
            if (!file) { finish({name:'', text:'',contentBase64:''}); return; }
            if (file.size > 2097152) throw new Error('账单文件不能超过 2 MiB');
            if (native) {
                const bytes = new Uint8Array(await file.arrayBuffer());
                let binary = '';
                for (let i=0; i<bytes.length; i+=32768) binary += String.fromCharCode(...bytes.subarray(i,i+32768));
                finish({name:file.name,contentBase64:btoa(binary)});
            } else finish({name:file.name, text:await file.text()});
        } catch (error) { input.remove(); reject(error); }
    };
    input.click();
})
""")
actual suspend fun chooseCSV(): PickedCSV? = apiJson.decodeFromString<PickedCSV>(selectCSV(false,false).await().toString()).takeIf { it.name.isNotEmpty() }

actual suspend fun chooseNativeBill(format:String): PickedAlipay? = apiJson.decodeFromString<PickedAlipay>(selectCSV(true,format=="wechat").await().toString()).takeIf { it.name.isNotEmpty() }

internal actual val loginClient:String = "web"
internal actual fun loginProof() = LoginProof("", "")
private fun navigateLogin(url:String):Unit = js("window.location.assign(url)")
internal actual suspend fun openLoginURL(url:String) {navigateLogin(url)}
internal actual suspend fun returnToApplication() { }

internal actual suspend fun loadSession(base:String):String? = null
internal actual suspend fun saveSession(base:String,token:String) {}
internal actual suspend fun clearSession(base:String) {}
