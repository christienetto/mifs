package com.mifs.android

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.Color
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

internal data class ArtworkData(val bitmap: Bitmap, val wash: Bitmap, val tint: Color)
internal fun JSONObject.artUrl(thumbnail: Boolean = false): String = optJSONObject("artwork")?.let {
    if (thumbnail) it.optString("thumbnailUrl", it.optString("url")) else it.optString("url")
}.orEmpty()

private object ArtworkCache {
    val cache = object : LruCache<String, ArtworkData>(24 * 1024 * 1024) {
        override fun sizeOf(key: String, value: ArtworkData) = value.bitmap.byteCount + value.wash.byteCount
    }
    suspend fun load(url: String): ArtworkData? = withContext(Dispatchers.IO) {
        cache.get(url)?.let { return@withContext it }
        val connection = URL(url).openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = 10_000; connection.readTimeout = 10_000
            val bytes = connection.inputStream.use { it.readBytes() }
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
            val options = BitmapFactory.Options().apply { inSampleSize = maxOf(1, maxOf(bounds.outWidth, bounds.outHeight) / 640) }
            val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, options) ?: return@withContext null
            val wash = Bitmap.createScaledBitmap(bitmap, 12, 12, true)
            val hues = FloatArray(3)
            val bins = FloatArray(12)
            for (x in 0 until 12) for (y in 0 until 12) {
                android.graphics.Color.colorToHSV(wash.getPixel(x, y), hues)
                bins[(hues[0] / 30).toInt().coerceIn(0, 11)] += hues[1] * hues[2]
            }
            val hue = bins.indices.maxByOrNull { bins[it] } ?: 9
            val tint = Color(android.graphics.Color.HSVToColor(floatArrayOf(hue * 30f + 15, .46f, .29f)))
            ArtworkData(bitmap, wash, tint).also { cache.put(url, it) }
        } finally { connection.disconnect() }
    }
}

@Composable
internal fun rememberArtwork(url: String): ArtworkData? {
    val art by produceState<ArtworkData?>(ArtworkCache.cache.get(url), url) {
        if (url.isNotEmpty()) {
            try { value = ArtworkCache.load(url) }
            catch (error: Exception) { if (error is kotlinx.coroutines.CancellationException) throw error }
        }
    }
    return art
}
