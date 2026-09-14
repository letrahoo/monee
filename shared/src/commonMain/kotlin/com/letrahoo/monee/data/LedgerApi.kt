package com.letrahoo.monee.data

import io.ktor.client.HttpClient
import io.ktor.client.request.*
import io.ktor.client.statement.bodyAsText
import io.ktor.http.*
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

internal val apiJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }
internal expect fun platformClient(): HttpClient
internal expect suspend fun discoverConnection(client: HttpClient): Connection
expect suspend fun chooseCSV(): PickedCSV?

class LedgerApi {
    private val client = platformClient()
    private var connection: Connection? = null

    fun reconnect() { connection = null }
    fun close() { client.close() }

    private suspend fun request(path: String, method: HttpMethod = HttpMethod.Get, body: String? = null,
                                parameters: Map<String, String> = emptyMap(), key: String? = null): String {
        try {
            repeat(2) { attempt ->
                val destination = connection ?: discoverConnection(client).also { connection = it }
                val response = client.request(destination.baseUrl + "/api/v1/" + path) {
                    this.method = method
                    header(HttpHeaders.Authorization, "Bearer ${destination.token}")
                    if (key != null) header("Idempotency-Key", key)
                    url { parameters.forEach { (name, value) -> this.parameters.append(name, value) } }
                    if (body != null) { contentType(ContentType.Application.Json); setBody(body) }
                }
                val text = response.bodyAsText()
                if (response.status == HttpStatusCode.Unauthorized && attempt == 0) {
                    connection = null
                } else {
                    if (!response.status.isSuccess()) {
                        val message = runCatching { apiJson.decodeFromString<APIProblem>(text).message }
                            .getOrDefault("本地服务返回异常，请重新连接")
                        throw LedgerException(message)
                    }
                    return text
                }
            }
            throw LedgerException("连接凭据失效，请重新连接")
        } catch (e: CancellationException) { throw e
        } catch (e: LedgerException) { throw e
        } catch (_: Exception) {
            connection = null
            throw LedgerException("无法连接本地账本。请确认 Monee 本地服务正在运行，再点击重新连接。")
        }
    }

    suspend fun dashboard(month: String, query: String, page: Int): Dashboard =
        apiJson.decodeFromString(request("dashboard", parameters = mapOf("month" to month, "q" to query, "page" to page.toString())))
    suspend fun preview(filename: String, csv: String): ImportPreview =
        apiJson.decodeFromString(request("imports/preview", HttpMethod.Post, apiJson.encodeToString(ImportRequest(filename,csv))))
    suspend fun commit(preview: ImportPreview, confirmSimilar: Boolean): CommitResult =
        apiJson.decodeFromString(request("imports/${preview.id}/commit", HttpMethod.Post, apiJson.encodeToString(CommitRequest(preview.ledgerVersion,confirmSimilar))))
    suspend fun create(input: TransactionInput, key: String): LedgerTransaction =
        apiJson.decodeFromString(request("transactions", HttpMethod.Post, apiJson.encodeToString(input), key = key))
    suspend fun template(sample: Boolean): String = request("template", parameters = mapOf("sample" to sample.toString()))
}

class LedgerException(message: String): Exception(message)
