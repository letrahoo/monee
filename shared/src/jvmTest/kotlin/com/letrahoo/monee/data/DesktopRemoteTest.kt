package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import java.nio.file.Files
import kotlin.test.*

class DesktopRemoteTest {
    private val origin = "https://monee.test.letra.xin"
    private val healthy = """{"status":"ok","apiVersion":1,"serviceProtocol":1,"schemaVersion":3}"""

    @Test fun explicitHttpsOriginsAreCanonicalAndCannotInjectPathsOrCredentials() {
        assertEquals(origin, DesktopRemote.origin("https://MONEE.test.letra.xin:443/"))
        assertEquals("https://example.com:8443", DesktopRemote.origin("https://example.com:8443"))
        for (bad in listOf("", " ", "http://127.0.0.1:4173", "http://monee.test.letra.xin",
            "https://user:password@monee.test.letra.xin", "$origin/path", "$origin?", "$origin#",
            "$origin:0", "$origin:65536", " $origin", "$origin/../", "https://[::1]", "https://example.com\\@evil.com")) {
            assertFailsWith<LedgerException>(bad) { DesktopRemote.origin(bad) }
        }
        assertNull(DesktopRemote.configured(null, null))
        assertFailsWith<LedgerException> { DesktopRemote.configured(null, "/unused") }
    }

    @Test fun invalidCaIsAnErrorNotDefaultTrustOrLocalFallback() {
        val file = Files.createTempFile("monee-ca-test-", ".pem")
        try {
            for (contents in listOf("", "not a certificate", "-----BEGIN PRIVATE KEY-----\nsynthetic")) {
                Files.writeString(file, contents)
                assertFailsWith<LedgerException> { DesktopRemote.configured(origin, file.toString()) }
            }
            assertFailsWith<LedgerException> { DesktopRemote.configured(origin, file.resolveSibling("missing-ca.pem").toString()) }
        } finally { Files.deleteIfExists(file) }
    }

    @Test fun remoteHealthUsesConfiguredOriginWithoutStartingLocalService() = runBlocking {
        var requests = 0
        val client = HttpClient(MockEngine { request ->
            assertEquals("$origin/api/v1/health", request.url.toString())
            assertNull(request.headers[HttpHeaders.Authorization])
            requests++
            respond(healthy)
        }) { followRedirects = false }
        try {
            assertEquals(Connection(origin), desktopConnection(client, DesktopRemote(origin)) { error("must not use local") })
            assertEquals(1, requests)
            assertEquals(Connection("http://127.0.0.1:4173"), desktopConnection(client, null) { Connection("http://127.0.0.1:4173") })
            assertEquals(1, requests)
        } finally { client.close() }
    }

    @Test fun incompatibleFailedAndRedirectedRemoteNeverFallsBackToLocal(): Unit = runBlocking {
        for ((status, body) in listOf(HttpStatusCode.Found to healthy, HttpStatusCode.Unauthorized to healthy,
            HttpStatusCode.OK to "not json", HttpStatusCode.OK to "{}",
            HttpStatusCode.OK to healthy.replace("\"apiVersion\":1", "\"apiVersion\":2"),
            HttpStatusCode.OK to healthy.replace("\"serviceProtocol\":1", "\"serviceProtocol\":2"))) {
            var calls = 0
            val client = HttpClient(MockEngine {
                calls++
                respond(body, status, headersOf(HttpHeaders.Location, "https://other.example/api/v1/health"))
            }) { followRedirects = false }
            try {
                assertFailsWith<LedgerException> { desktopConnection(client, DesktopRemote(origin)) { error("local fallback") } }
                assertEquals(1, calls)
            } finally { client.close() }
        }
        val client = HttpClient(MockEngine { throw CancellationException("synthetic cancellation") })
        try {
            assertFailsWith<CancellationException> { desktopConnection(client, DesktopRemote(origin)) { error("local fallback") } }
        } finally { client.close() }
    }

    @Test fun remoteAndLocalSessionsHaveDifferentKeychainAccounts() {
        assertNotEquals(SessionVault.account(origin, "/synthetic"), SessionVault.account("http://127.0.0.1:4173", "/synthetic"))
        assertNotEquals(SessionVault.account(origin, "/synthetic"), SessionVault.account(origin, "/other-synthetic"))
    }

    @Test fun liveRemoteConnectionWhenExplicitlyRequested() = runBlocking {
        if (System.getenv("MONEE_TEST_REMOTE") != "1") return@runBlocking
        val client = platformClient()
        try {
            assertEquals(DesktopRemote.origin(System.getenv("MONEE_SERVER_URL")), discoverConnection(client).baseUrl)
        } finally { client.close() }
    }
}
