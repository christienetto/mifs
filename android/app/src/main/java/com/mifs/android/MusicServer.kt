package com.mifs.android

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

fun JSONArray.objects() = (0 until length()).map { getJSONObject(it) }

class MusicServer(val base: String) {
    suspend fun request(path: String, body: JSONObject? = null): JSONObject = withContext(Dispatchers.IO) {
        val connection = URL(base.trimEnd('/') + path).openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = 10_000
            connection.readTimeout = 25_000
            connection.setRequestProperty("Accept", "application/json")
            if (body != null) {
                connection.requestMethod = "POST"
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val status = connection.responseCode
            val raw = (if (status in 200..299) connection.inputStream else connection.errorStream)
                ?.bufferedReader()?.use { it.readText() }.orEmpty()
            val json = if (raw.isBlank()) JSONObject() else JSONObject(raw)
            if (status !in 200..299) throw ApiException(status,
                json.optJSONObject("error")?.optString("message") ?: "Music server returned HTTP $status")
            json
        } finally { connection.disconnect() }
    }
}
class ApiException(val status: Int, message: String) : Exception(message)
