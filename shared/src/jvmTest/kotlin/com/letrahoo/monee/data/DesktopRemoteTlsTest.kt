package com.letrahoo.monee.data

import com.sun.net.httpserver.HttpsConfigurator
import com.sun.net.httpserver.HttpsServer
import io.ktor.client.request.*
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.runBlocking
import java.net.InetSocketAddress
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyStore
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext
import kotlin.test.*

class DesktopRemoteTlsTest {
    @Test fun realTlsChecksTrustAndHostnameAndNeverRedirectsCredentials(): Unit = runBlocking {
        val directory = Files.createTempDirectory("monee-synthetic-tls-")
        val server = HttpsServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        val capture = HttpsServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        try {
            val password = "synthetic-test-only" // Disposable test keystore, never a real credential.
            val keytool = Path.of(System.getProperty("java.home"), "bin", "keytool").toString()
            val storePath = directory.resolve("server.p12")
            fun keytool(vararg args: String) {
                val process = ProcessBuilder(listOf(keytool) + args)
                    .redirectOutput(ProcessBuilder.Redirect.DISCARD).redirectError(ProcessBuilder.Redirect.DISCARD).start()
                if (!process.waitFor(30, TimeUnit.SECONDS)) { process.destroyForcibly(); error("Synthetic keytool timeout") }
                check(process.exitValue() == 0) { "Synthetic keytool failed" }
            }
            keytool("-genkeypair", "-alias", "test", "-keystore", storePath.toString(), "-storetype", "PKCS12",
                "-storepass", password, "-keyalg", "RSA", "-keysize", "2048", "-dname", "CN=localhost",
                "-validity", "2", "-ext", "BC=ca:true", "-ext", "SAN=dns:localhost", "-noprompt")
            val ca = directory.resolve("ca.pem")
            keytool("-exportcert", "-rfc", "-alias", "test", "-keystore", storePath.toString(), "-storepass", password, "-file", ca.toString())
            val store = KeyStore.getInstance("PKCS12").apply { Files.newInputStream(storePath).use { load(it, password.toCharArray()) } }
            val keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()).apply { init(store, password.toCharArray()) }
            val tls = SSLContext.getInstance("TLS").apply { init(keys.keyManagers, null, null) }
            server.httpsConfigurator = HttpsConfigurator(tls)
            capture.httpsConfigurator = HttpsConfigurator(tls)
            val captured = AtomicInteger()
            capture.createContext("/") { exchange -> captured.incrementAndGet(); exchange.sendResponseHeaders(200, -1); exchange.close() }
            capture.start()
            server.createContext("/api/v1/health") { exchange ->
                val body = """{"status":"ok","apiVersion":1,"serviceProtocol":1}""".toByteArray()
                exchange.sendResponseHeaders(200, body.size.toLong())
                exchange.responseBody.use { it.write(body) }
            }
            for (status in listOf(307, 308)) server.createContext("/redirect-$status") { exchange ->
                exchange.responseHeaders.set("Location", "https://localhost:${capture.address.port}/capture")
                exchange.sendResponseHeaders(status, -1); exchange.close()
            }
            server.start()
            val origin = "https://localhost:${server.address.port}"
            val remote = DesktopRemote.configured(origin, ca.toString())!!
            val client = desktopHttpClient(Result.success(remote))
            try {
                assertEquals(Connection(origin), desktopConnection(client, remote) { error("local fallback") })
                // The very same trusted certificate must fail for a hostname absent from its SAN.
                assertFailsWith<Exception> { DesktopRemote("https://127.0.0.1:${server.address.port}", remote.trustManager).connect(client) }
                for (status in listOf(307, 308)) {
                    val response = client.post("$origin/redirect-$status") {
                        header(HttpHeaders.Authorization, "Bearer synthetic-only")
                        setBody("synthetic-proof-only")
                    }
                    assertEquals(HttpStatusCode.fromValue(status), response.status)
                }
                assertEquals(0, captured.get())
            } finally { client.close() }
            val untrusted = desktopHttpClient(Result.success(DesktopRemote(origin)))
            try { assertFailsWith<Exception> { remote.connect(untrusted) } } finally { untrusted.close() }
            val invalid = desktopHttpClient(Result.failure(LedgerException("synthetic bad configuration")))
            try { assertFailsWith<Exception> { remote.connect(invalid) } } finally { invalid.close() }
            // Expired CA input is rejected before any connection or default-trust fallback.
            keytool("-genkeypair", "-alias", "expired", "-keystore", directory.resolve("expired.p12").toString(),
                "-storepass", password, "-keyalg", "RSA", "-keysize", "2048", "-dname", "CN=expired",
                "-startdate", "-3d", "-validity", "1", "-ext", "BC=ca:true", "-noprompt")
            keytool("-exportcert", "-rfc", "-alias", "expired", "-keystore", directory.resolve("expired.p12").toString(),
                "-storepass", password, "-file", directory.resolve("expired.pem").toString())
            assertFailsWith<LedgerException> { DesktopRemote.configured(origin, directory.resolve("expired.pem").toString()) }
        } finally {
            server.stop(0); capture.stop(0)
            Files.walk(directory).use { paths -> paths.sorted(Comparator.reverseOrder()).forEach { Files.deleteIfExists(it) } }
        }
    }
}
