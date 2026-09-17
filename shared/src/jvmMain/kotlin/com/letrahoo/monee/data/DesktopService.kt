package com.letrahoo.monee.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.net.HttpURLConnection
import java.net.URI
import java.nio.channels.FileChannel
import java.nio.channels.OverlappingFileLockException
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardOpenOption
import kotlin.coroutines.coroutineContext

/** Discovery is a process identity, never an authorization credential. */
internal data class ServiceDiscovery(val baseUrl: String, val instanceId: String, val serviceProtocol: Int) {
    fun valid(): Boolean = runCatching {
        val uri = URI(baseUrl)
        uri.scheme == "http" && uri.host == "127.0.0.1" && uri.port in 1..65535 &&
            uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
            uri.rawPath.orEmpty().isEmpty() && instanceId.matches(Regex("[0-9a-f]{64}")) && serviceProtocol == 1
    }.getOrDefault(false)
}
internal sealed interface ServiceProbe {
    data object Unreachable : ServiceProbe
    data object Unrecognized : ServiceProbe
    data class Ready(val identity: ServiceDiscovery) : ServiceProbe
}
internal interface DesktopServiceHost {
    fun discovery(): ServiceDiscovery?
    fun probe(baseUrl: String): ServiceProbe
    /** The returned function only observes process liveness. Never terminates it. */
    fun launch(): () -> Boolean
}

/** A bounded start/reuse state machine, with all external effects injectable for tests. */
internal class DesktopServiceResolver(private val host: DesktopServiceHost, private val attempts: Int = 40,
                                      private val pause: suspend () -> Unit = { delay(250) }) {
    companion object { const val DEFAULT_URL = "http://127.0.0.1:4173" }
    private fun existing(): ServiceDiscovery? {
        val discovered = host.discovery()
        if (discovered != null && !discovered.copy(instanceId = "a".repeat(64), serviceProtocol = 1).valid()) {
            throw LedgerException("本地服务地址无效。请检查数据目录中的服务配置后重试。")
        }
        val origin = discovered?.baseUrl ?: DEFAULT_URL
        return when (val health = host.probe(origin)) {
            ServiceProbe.Unreachable -> null
            ServiceProbe.Unrecognized -> throw LedgerException("本地地址已被其他服务占用。请检查后重试。")
            is ServiceProbe.Ready -> {
                if (discovered == null || discovered != health.identity || !health.identity.valid()) {
                    throw LedgerException("本地服务信息不匹配。请确认正在使用同一个数据目录后重试。")
                }
                discovered
            }
        }
    }
    suspend fun resolve(): ServiceDiscovery {
        coroutineContext.ensureActive()
        existing()?.let { return it }
        // A stale discovery may refer to a different port. OAuth uses the fixed default port.
        val old = host.discovery()
        if (old != null && old.baseUrl != DEFAULT_URL && host.probe(DEFAULT_URL) != ServiceProbe.Unreachable) {
            throw LedgerException("登录所需的本地地址已被占用。请检查后重试。")
        }
        val alive = host.launch()
        repeat(attempts) {
            coroutineContext.ensureActive()
            existing()?.let { return it }
            if (!alive()) throw LedgerException("本地服务未能启动。请检查登录配置和数据目录后重试。")
            pause()
        }
        throw LedgerException("本地服务启动超时，请稍后重试。")
    }
}

internal object DesktopService {
    private val mutex = Mutex()
    suspend fun connection(dataDir: Path): Connection = withContext(Dispatchers.IO) {
        mutex.withLock {
            Files.createDirectories(dataDir)
            // Separate from the Go lifetime lock. Concurrent desktop launches wait, then re-probe.
            FileChannel.open(dataDir.resolve("desktop-start.lock"), StandardOpenOption.CREATE, StandardOpenOption.WRITE).use { channel ->
                var acquired: java.nio.channels.FileLock? = null
                repeat(80) {
                    if (acquired == null) {
                        coroutineContext.ensureActive()
                        acquired = try { channel.tryLock() } catch (_: OverlappingFileLockException) { null }
                        if (acquired == null) delay(250)
                    }
                }
                val lock = acquired ?: throw LedgerException("另一个窗口正在启动服务，请稍后重试。")
                lock.use {
                    val record = DesktopServiceResolver(LocalDesktopServiceHost(dataDir)).resolve()
                    Connection(record.baseUrl)
                }
            }
        }
    }
}

internal class LocalDesktopServiceHost(private val dataDir: Path) : DesktopServiceHost {
    override fun discovery(): ServiceDiscovery? {
        val path = dataDir.resolve("connection.json")
        if (!Files.exists(path)) return null
        return try {
            require(Files.size(path) <= 4096)
            val value = apiJson.parseToJsonElement(Files.readString(path)).jsonObject
            ServiceDiscovery(value.getValue("baseUrl").jsonPrimitive.content,
                value["instanceId"]?.jsonPrimitive?.content.orEmpty(),
                value["serviceProtocol"]?.jsonPrimitive?.intOrNull ?: 0)
        } catch (_: Exception) {
            throw LedgerException("本地服务信息已过期或损坏。请退出旧服务并检查数据目录后重试。")
        }
    }
    override fun probe(baseUrl: String): ServiceProbe {
        val connection = URI("$baseUrl/api/v1/health").toURL().openConnection() as HttpURLConnection
        connection.connectTimeout = 1000
        connection.readTimeout = 1000
        connection.instanceFollowRedirects = false
        var connected = false
        return try {
            connection.connect()
            connected = true
            // Once HTTP responds, malformed / incompatible services must never be replaced automatically.
            val status = connection.responseCode
            if (status != 200) return ServiceProbe.Unrecognized
            try {
                val body = connection.inputStream.use { it.readNBytes(4097) }
                if (body.size > 4096) return ServiceProbe.Unrecognized
                val value = apiJson.parseToJsonElement(body.toString(Charsets.UTF_8)).jsonObject
                if (value["status"]?.jsonPrimitive?.content != "ok") return ServiceProbe.Unrecognized
                val identity = ServiceDiscovery(baseUrl, value.getValue("instanceId").jsonPrimitive.content,
                    value.getValue("serviceProtocol").jsonPrimitive.intOrNull ?: 0)
                if (identity.valid()) ServiceProbe.Ready(identity) else ServiceProbe.Unrecognized
            } catch (_: Exception) { ServiceProbe.Unrecognized }
        } catch (_: java.io.IOException) { if (connected) ServiceProbe.Unrecognized else ServiceProbe.Unreachable }
        finally { connection.disconnect() }
    }
    override fun launch(): () -> Boolean {
        val root = System.getProperty("compose.application.resources.dir")?.takeIf { it.isNotBlank() }?.let(Path::of)
            ?: throw LedgerException("请先启动本地服务，或安装包含服务的 Monee 应用。")
        val executable = root.resolve("monee-service/monee")
        val web = root.resolve("monee-service/web")
        if (!Files.isRegularFile(executable) || !Files.isExecutable(executable) || !Files.isRegularFile(web.resolve("index.html"))) {
            throw LedgerException("应用中的本地服务不完整，请重新安装 Monee 后重试。")
        }
        val process = try {
            ProcessBuilder(executable.toAbsolutePath().toString(), "-data-dir", dataDir.toAbsolutePath().toString(),
                "-listen", "127.0.0.1:4173", "-web-dir", web.toAbsolutePath().toString())
                .redirectInput(ProcessBuilder.Redirect.PIPE).redirectOutput(ProcessBuilder.Redirect.DISCARD)
                .redirectError(ProcessBuilder.Redirect.DISCARD).start().also { it.outputStream.close() }
        } catch (_: java.io.IOException) { throw LedgerException("无法启动本地服务，请检查应用是否安装完整后重试。") }
        // Deliberately no shutdown hook: Web and other clients share this process.
        return { process.isAlive }
    }
}
