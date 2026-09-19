package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.request.get
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpStatusCode
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyStore
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

/** Explicit remote mode is separate from local process discovery and never starts a service. */
internal data class DesktopRemote(val origin: String, val trustManager: X509TrustManager? = null) {
    companion object {
        fun origin(value: String): String {
            val uri = runCatching { URI(value) }.getOrNull()
            if (uri == null || uri.scheme != "https" || uri.host.isNullOrBlank() || uri.host.contains(':') ||
                uri.rawUserInfo != null || uri.rawQuery != null || uri.rawFragment != null ||
                uri.rawPath.orEmpty() !in listOf("", "/") ||
                (uri.port != -1 && uri.port !in 1..65535) || value.any { it.isWhitespace() }) {
                throw LedgerException("测试服务器地址必须是完整 HTTPS 域名，不含账号、路径或查询参数。")
            }
            val host = uri.host.lowercase()
            return URI("https", null, host, if (uri.port == 443) -1 else uri.port, null, null, null).toASCIIString()
        }

        fun configured(server: String?, caFile: String?): DesktopRemote? {
            if (server == null) {
                if (caFile != null) throw LedgerException("自定义证书只能用于明确配置的 HTTPS 测试服务器。")
                return null
            }
            val origin = origin(server)
            val trust = caFile?.let { path ->
                try {
                    val file = Path.of(path)
                    require(Files.size(file) in 1..65_536)
                    val pem = Files.readString(file)
                    require(!pem.contains("PRIVATE KEY"))
                    val certificates = CertificateFactory.getInstance("X.509")
                        .generateCertificates(pem.byteInputStream()).map { it as X509Certificate }
                    require(certificates.isNotEmpty() && certificates.all { it.basicConstraints >= 0 })
                    val store = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null, null) }
                    certificates.forEachIndexed { index, certificate ->
                        certificate.checkValidity()
                        store.setCertificateEntry("server-ca-$index", certificate)
                    }
                    val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
                    factory.init(store)
                    factory.trustManagers.filterIsInstance<X509TrustManager>().single()
                } catch (_: Exception) {
                    throw LedgerException("测试服务器 CA 证书无效或不可读。请提供公开 CA 证书，不能使用私钥或跳过校验。")
                }
            }
            return DesktopRemote(origin, trust)
        }
    }

    suspend fun connect(client: HttpClient): Connection {
        // Use the same TLS policy as subsequent API calls. No local identity-file requirement.
        val response = client.get("$origin/api/v1/health")
        if (response.status != HttpStatusCode.OK) throw LedgerException("测试服务器不可用或发生重定向，请检查配置。")
        val body = response.bodyAsText()
        val compatible = runCatching {
            val health = apiJson.parseToJsonElement(body).jsonObject
            health["status"]?.jsonPrimitive?.content == "ok" &&
                health["apiVersion"]?.jsonPrimitive?.intOrNull == 1 &&
                health["serviceProtocol"]?.jsonPrimitive?.intOrNull == 1
        }.getOrDefault(false)
        if (!compatible) {
            throw LedgerException("测试服务器协议不兼容，请检查服务版本。")
        }
        return Connection(origin)
    }
}

internal object DesktopRemoteConfiguration {
    // Read once per process. An invalid explicit setting must never silently use the local ledger.
    val current: Result<DesktopRemote?> by lazy {
        runCatching { DesktopRemote.configured(System.getenv("MONEE_SERVER_URL"), System.getenv("MONEE_SERVER_CA_FILE")) }
    }
    val rejectInvalidConfiguration = object : X509TrustManager {
        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) { throw CertificateException("Invalid server configuration") }
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) { throw CertificateException("Invalid server configuration") }
    }
}

internal suspend fun desktopConnection(client: HttpClient, remote: DesktopRemote?, local: suspend () -> Connection): Connection =
    if (remote != null) remote.connect(client) else local()
