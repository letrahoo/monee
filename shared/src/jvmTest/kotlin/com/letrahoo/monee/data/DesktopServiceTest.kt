package com.letrahoo.monee.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class DesktopServiceTest {
    private val expected = ServiceDiscovery(DesktopServiceResolver.DEFAULT_URL, "a".repeat(64), 1)
    private class Host(var record: ServiceDiscovery?, var result: ServiceProbe) : DesktopServiceHost {
        var launches = 0
        var onLaunch: () -> Unit = {}
        var alive = true
        override fun discovery() = record
        override fun probe(baseUrl: String) = result
        override fun launch(): () -> Boolean { launches++; onLaunch(); return { alive } }
    }
    @Test fun healthyMatchingIdentityReusesWithoutLaunching() = runBlocking {
        val host = Host(expected, ServiceProbe.Ready(expected))
        assertEquals(expected, DesktopServiceResolver(host).resolve())
        assertEquals(0, host.launches)
    }
    @Test fun absentOrStaleUnreachableServiceStartsAndWaitsForMatchingIdentity() = runBlocking {
        for (stale in listOf(null, expected, expected.copy(instanceId = "", serviceProtocol = 0))) {
            val host = Host(stale, ServiceProbe.Unreachable)
            host.onLaunch = { host.record = expected; host.result = ServiceProbe.Ready(expected) }
            assertEquals(expected, DesktopServiceResolver(host).resolve())
            assertEquals(1, host.launches)
        }
    }
    @Test fun reachableOldForeignAndMismatchedServicesAreNeverLaunchedOver() = runBlocking {
        for (host in listOf(
            Host(expected, ServiceProbe.Unrecognized),
            Host(expected, ServiceProbe.Ready(expected.copy(instanceId = "b".repeat(64)))),
            Host(null, ServiceProbe.Ready(expected)),
            Host(expected.copy(serviceProtocol = 2), ServiceProbe.Ready(expected.copy(serviceProtocol = 2))),
            Host(expected.copy(instanceId = "", serviceProtocol = 0), ServiceProbe.Ready(expected))
        )) {
            assertFailsWith<LedgerException> { DesktopServiceResolver(host).resolve() }
            assertEquals(0, host.launches)
        }
    }
    @Test fun readinessIsBoundedAndFailedStartCanBeRetried() = runBlocking {
        val host = Host(null, ServiceProbe.Unreachable)
        var waits = 0
        assertFailsWith<LedgerException> { DesktopServiceResolver(host, 3) { waits++ }.resolve() }
        assertEquals(3, waits)
        host.alive = false
        assertFailsWith<LedgerException> { DesktopServiceResolver(host, 3) { waits++ }.resolve() }
        assertEquals(3, waits)
        host.onLaunch = { host.alive = true; host.record = expected; host.result = ServiceProbe.Ready(expected) }
        assertEquals(expected, DesktopServiceResolver(host).resolve())
    }
    @Test fun cancellationIsNotSwallowed(): Unit = runBlocking {
        val host = Host(null, ServiceProbe.Unreachable)
        assertFailsWith<CancellationException> {
            DesktopServiceResolver(host) { throw CancellationException("synthetic cancellation") }.resolve()
        }
    }
    @Test fun discoveryFileHandlesMissingLegacyAndMalformedRecords() {
        val directory = java.nio.file.Files.createTempDirectory("monee-service-test-")
        try {
            val host = LocalDesktopServiceHost(directory)
            assertEquals(null, host.discovery())
            val file = directory.resolve("connection.json")
            java.nio.file.Files.writeString(file, """{"baseUrl":"http://127.0.0.1:4173"}""")
            assertEquals(expected.copy(instanceId = "", serviceProtocol = 0), host.discovery())
            java.nio.file.Files.writeString(file, """{"baseUrl":"http://127.0.0.1:4173","instanceId":"${expected.instanceId}","serviceProtocol":1}""")
            assertEquals(expected, host.discovery())
            java.nio.file.Files.writeString(file, "not JSON")
            assertFailsWith<LedgerException> { host.discovery() }
            java.nio.file.Files.writeString(file, "x".repeat(4097))
            assertFailsWith<LedgerException> { host.discovery() }
        } finally {
            java.nio.file.Files.deleteIfExists(directory.resolve("connection.json"))
            java.nio.file.Files.deleteIfExists(directory)
        }
    }
    @Test fun discoveryRestrictsAddressAndIdentity() {
        assertTrue(expected.valid())
        listOf("https://127.0.0.1:4173", "http://example.com:4173", "http://127.0.0.1:4173/path",
            "http://user@127.0.0.1:4173", "http://127.0.0.1:4173?x=1", "http://127.0.0.1:4173#x").forEach {
            assertFalse(expected.copy(baseUrl = it).valid())
        }
        assertFalse(expected.copy(instanceId = "").valid())
        assertFalse(expected.copy(serviceProtocol = 0).valid())
    }
}
