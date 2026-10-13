package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.*
import io.ktor.http.content.TextContent
import kotlinx.coroutines.*
import kotlinx.serialization.encodeToString
import java.io.IOException
import kotlin.test.*

/** Synthetic transport only: no live service, identity provider or keychain. */
class TransferApiTest {
    private val input = TransferInput("2026-10-13", "100.01", "QA-A", "QA-B", note="synthetic")
    private val transaction = LedgerTransaction("synthetic-transfer", input.date, "transfer", "10001",
        "CNY", "本人账户转账", "本人转账", "手动转账", input.fromAccount, "", input.note, 1, toAccount=input.toAccount)

    private class Fixture {
        var origin = "https://synthetic.invalid"
        var user = "synthetic-user-a"
        data class Sent(val url: String, val key: String?, val body: String,
            val ledger: String?, val authorization: String?, val csrf: String?, val method: HttpMethod)
        val sent = mutableListOf<Sent>()
        val vault = mutableMapOf(origin to "synthetic-token-a")
        val removedSessions = mutableListOf<String>()
        var submit: suspend () -> Pair<HttpStatusCode, String> = {
            HttpStatusCode.Created to "{}"
        }
        val client = HttpClient(MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/v1/auth/me" -> respond(apiJson.encodeToString(AuthState(
                    AuthUser(user, "github", "synthetic-sub", allowed=true), csrfToken="synthetic-csrf")))
                "/api/v1/auth/logout" -> respond("{}")
                "/api/v1/transfers" -> {
                    sent += Sent(request.url.toString(), request.headers["Idempotency-Key"],
                        (request.body as TextContent).text, request.headers["X-Monee-Ledger"],
                        request.headers[HttpHeaders.Authorization], request.headers["X-Monee-CSRF"], request.method)
                    assertEquals(HttpMethod.Post, request.method)
                    assertEquals("synthetic-csrf", request.headers["X-Monee-CSRF"])
                    val (status, body) = submit()
                    respond(body, status)
                }
                else -> error("unexpected synthetic request ${request.url}")
            }
        })
        val api = LedgerApi(client, { Connection(origin) }, { vault[it] },
            { base, token -> vault[base] = token }, { base -> removedSessions += base; vault.remove(base) })
    }

    @Test fun unknownResponsesRetainExactSubmissionUntilSuccessfulRetry(): Unit = runBlocking {
        for (failure in listOf("lost", "server", "malformed")) {
            val f = Fixture()
            try {
                f.api.authState()
                val first = f.api.prepareTransfer("ledger-a", input) { "synthetic-stable-key" }
                f.submit = {
                    when (failure) {
                        "lost" -> throw IOException("synthetic response lost after commit")
                        "server" -> HttpStatusCode.InternalServerError to """{"code":"error","message":"synthetic failure"}"""
                        else -> HttpStatusCode.Created to "not json"
                    }
                }
                assertFailsWith<Exception> { f.api.createTransfer("ledger-a", first) }
                assertSame(first, f.api.pendingTransfer("ledger-a"))
                assertSame(first, f.api.prepareTransfer("ledger-a", input.copy(amount="999")) {
                    error("uncertain request key replaced")
                })
                f.submit = { HttpStatusCode.Created to apiJson.encodeToString(transaction) }
                assertEquals(transaction, f.api.createTransfer("ledger-a", first))
                assertNull(f.api.pendingTransfer("ledger-a"))
                assertEquals(2, f.sent.size)
                assertEquals(f.sent[0], f.sent[1])
                assertEquals(apiJson.encodeToString(input), f.sent[1].body)
                assertEquals("ledger-a", f.sent[1].ledger)
                assertEquals("Bearer synthetic-token-a", f.sent[1].authorization)
            } finally { f.api.close() }
        }
    }

    @Test fun cancellationAfterRequestStartedPreservesOriginalRetry(): Unit = runBlocking {
        val f = Fixture()
        try {
            f.api.authState()
            val first = f.api.prepareTransfer("ledger-a", input) { "synthetic-cancel-key" }
            val started = CompletableDeferred<Unit>()
            f.submit = { started.complete(Unit); awaitCancellation() }
            val write = async { f.api.createTransfer("ledger-a", first) }
            withTimeout(5_000) { started.await() }
            write.cancel(CancellationException("synthetic caller cancellation"))
            assertFailsWith<CancellationException> { write.await() }
            assertSame(first, f.api.pendingTransfer("ledger-a"))
            f.submit = { HttpStatusCode.Created to apiJson.encodeToString(transaction) }
            assertEquals(transaction, f.api.createTransfer("ledger-a", first))
            assertEquals(f.sent[0], f.sent[1])
            assertNull(f.api.pendingTransfer("ledger-a"))
        } finally { f.api.close() }
    }

    @Test fun onlyDefinitiveValidationOrConflictClearsPending(): Unit = runBlocking {
        for ((code, status) in listOf("invalid" to HttpStatusCode.UnprocessableEntity,
            "conflict" to HttpStatusCode.Conflict, "ledger_readonly" to HttpStatusCode.Forbidden,
            "access_denied" to HttpStatusCode.Forbidden, "unauthenticated" to HttpStatusCode.Unauthorized)) {
            val f = Fixture()
            try {
                f.api.authState()
                val first = f.api.prepareTransfer("ledger-a", input) { "synthetic-error-key" }
                f.submit = { status to """{"code":"$code","message":"synthetic rejection"}""" }
                assertEquals(code, assertFailsWith<LedgerException> { f.api.createTransfer("ledger-a", first) }.code)
                if (code in listOf("invalid", "conflict")) assertNull(f.api.pendingTransfer("ledger-a"))
                else assertSame(first, f.api.pendingTransfer("ledger-a"))
            } finally { f.api.close() }
        }
    }

    @Test fun realSessionAndOriginSwitchesCannotSendOrClearAnotherScope(): Unit = runBlocking {
        val f = Fixture()
        try {
            f.api.authState()
            val first = f.api.prepareTransfer("ledger-a", input) { "synthetic-scope-key" }
            assertFailsWith<IllegalArgumentException> { f.api.createTransfer("ledger-b", first) }
            assertTrue(f.sent.isEmpty())
            f.api.logout()
            assertEquals(listOf(f.origin), f.removedSessions)
            assertTrue(f.vault.isEmpty())
            assertNull(f.api.pendingTransfer("ledger-a"))
            f.user = "synthetic-user-b"
            f.api.authState()
            assertNull(f.api.pendingTransfer("ledger-a"))
            assertFailsWith<IllegalArgumentException> { f.api.createTransfer("ledger-a", first) }
            f.user = "synthetic-user-a"
            f.api.authState()
            assertSame(first, f.api.pendingTransfer("ledger-a"))
            val originalOrigin = f.origin
            f.origin = "https://other-synthetic.invalid"
            f.api.reconnect()
            f.api.authState()
            assertNull(f.api.pendingTransfer("ledger-a"))
            assertFailsWith<IllegalArgumentException> { f.api.createTransfer("ledger-a", first) }
            assertTrue(f.sent.isEmpty())
            f.origin = originalOrigin
            f.api.reconnect()
            f.api.authState()
            assertSame(first, f.api.pendingTransfer("ledger-a"))
            val separate = f.api.prepareTransfer("ledger-b", input) { "synthetic-other-ledger-key" }
            f.submit = { HttpStatusCode.Created to apiJson.encodeToString(transaction) }
            f.api.createTransfer("ledger-b", separate)
            assertSame(first, f.api.pendingTransfer("ledger-a"))
            f.api.createTransfer("ledger-a", first)
            assertEquals(listOf("ledger-b", "ledger-a"), f.sent.map { it.ledger })
            assertTrue(f.sent.all { it.authorization == null }) // logout cleared the in-memory credential
            assertNull(f.api.pendingTransfer("ledger-a"))
        } finally { f.api.close() }
    }
}
