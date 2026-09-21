package com.mifs.android

import java.net.URI

/** The same server-mif code used by iOS and the Telegram Mini App. */
internal object TelegramLink {
    private const val bot = "MIFSAppBot"
    fun startParameter(page: String): String? {
        val url = try { URI(page) } catch (_: Exception) { return null }
        // The bot resolves IDs on the public server, so local/test-server IDs cannot be sent here.
        if (url.scheme != "https" || url.host != "mifs.cgn.fi" || url.port !in listOf(-1, 443) || url.userInfo != null) return null
        val id = Regex("^/m/([a-z2-7]{12})$").matchEntire(url.path.orEmpty())?.groupValues?.get(1) ?: return null
        return "s2_$id"
    }
    fun appURL(parameter: String) = "tg://resolve?domain=$bot&startapp=$parameter&mode=compact"
    fun webURL(parameter: String) = "https://t.me/$bot?startapp=$parameter&mode=compact"
}
