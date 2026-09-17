package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.cio.CIO
import io.ktor.client.plugins.HttpTimeout
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.swing.Swing
import kotlinx.coroutines.withContext
import java.awt.FileDialog
import java.awt.Frame
import java.nio.file.Files
import java.nio.file.Path

internal actual fun platformClient() = HttpClient(CIO) {
    install(HttpTimeout) { requestTimeoutMillis = 20_000; connectTimeoutMillis = 5_000 }
}

internal fun desktopDataDirectory():Path = System.getenv("MONEE_DATA_DIR")?.let { Path.of(it) } ?: run {
        val home = Path.of(System.getProperty("user.home"))
        if (System.getProperty("os.name").startsWith("Mac")) home.resolve("Library/Application Support/Monee")
        else (System.getenv("XDG_CONFIG_HOME")?.let { Path.of(it) } ?: home.resolve(".config")).resolve("Monee")
    }

internal actual suspend fun discoverConnection(client: HttpClient): Connection = withContext(Dispatchers.IO) {
    val dataDir = desktopDataDirectory()
    val connection = apiJson.decodeFromString<Connection>(Files.readString(dataDir.resolve("connection.json")))
    val uri = java.net.URI(connection.baseUrl)
    require(uri.scheme == "http" && uri.host == "127.0.0.1" && uri.port in 1..65535)
    connection
}

actual suspend fun chooseCSV(): PickedCSV? {
    val selected = withContext(Dispatchers.Swing) {
        val dialog = FileDialog(null as Frame?, "选择 UTF-8 标准 CSV 账单", FileDialog.LOAD)
        try {
            dialog.setFilenameFilter { _, name -> name.endsWith(".csv", ignoreCase = true) }
            dialog.isVisible = true
            dialog.file?.let { Path.of(dialog.directory, it) }
        } finally { dialog.dispose() }
    } ?: return null
    return withContext(Dispatchers.IO) {
        if (Files.size(selected) > 2 * 1024 * 1024) throw LedgerException("CSV 文件不能超过 2 MiB")
        PickedCSV(selected.fileName.toString(), Files.readString(selected))
    }
}

internal actual fun isPlatformNetworkFailure(cause: Throwable): Boolean = false
internal actual val loginClient:String = "desktop"
internal actual fun loginProof():LoginProof {
    val bytes=ByteArray(32).also {java.security.SecureRandom().nextBytes(it)}
    val encoder=java.util.Base64.getUrlEncoder().withoutPadding()
    val verifier=encoder.encodeToString(bytes)
    val challenge=encoder.encodeToString(java.security.MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII)))
    return LoginProof(verifier,challenge)
}
internal actual suspend fun openLoginURL(url:String) = withContext(Dispatchers.IO) {
    java.awt.Desktop.getDesktop().browse(java.net.URI(url))
}
internal actual suspend fun returnToApplication() { DesktopReturn.activate() }

actual suspend fun chooseNativeBill(format:String): PickedAlipay? {
    val selected = withContext(Dispatchers.Swing) {
        val dialog = FileDialog(null as Frame?, if(format=="wechat")"选择微信原始 XLSX 账单" else "选择支付宝原始 CSV 账单", FileDialog.LOAD)
        try {
            dialog.setFilenameFilter { _, name -> name.endsWith(if(format=="wechat")".xlsx"else".csv", ignoreCase = true) }
            dialog.isVisible = true
            dialog.file?.let { Path.of(dialog.directory, it) }
        } finally { dialog.dispose() }
    } ?: return null
    return withContext(Dispatchers.IO) {
        if (Files.size(selected) > 2 * 1024 * 1024) throw LedgerException("账单文件不能超过 2 MiB")
        PickedAlipay(selected.fileName.toString(), java.util.Base64.getEncoder().encodeToString(Files.readAllBytes(selected)))
    }
}
