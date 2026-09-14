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

private fun selectCSV(): Promise<JsString> = js("""
new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file'; input.accept = '.csv,text/csv'; input.style.display = 'none';
    document.body.appendChild(input);
    const finish = value => { input.remove(); resolve(JSON.stringify(value)); };
    input.oncancel = () => finish({name:'', text:''});
    input.onchange = async () => {
        try {
            const file = input.files[0];
            if (!file) { finish({name:'', text:''}); return; }
            if (file.size > 2097152) throw new Error('CSV 文件不能超过 2 MiB');
            finish({name:file.name, text:await file.text()});
        } catch (error) { input.remove(); reject(error); }
    };
    input.click();
})
""")
actual suspend fun chooseCSV(): PickedCSV? = apiJson.decodeFromString<PickedCSV>(selectCSV().await().toString()).takeIf { it.name.isNotEmpty() }

internal actual val loginClient:String = "web"
internal actual fun loginProof() = LoginProof("", "")
private fun navigateLogin(url:String):Unit = js("window.location.assign(url)")
internal actual suspend fun openLoginURL(url:String) {navigateLogin(url)}
