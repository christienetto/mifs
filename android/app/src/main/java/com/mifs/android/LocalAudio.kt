package com.mifs.android

import android.content.Context
import android.media.AudioFormat
import android.media.MediaCodec
import android.media.MediaExtractor
import android.media.MediaFormat
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.transformer.Composition
import androidx.media3.transformer.EditedMediaItem
import androidx.media3.transformer.ExportException
import androidx.media3.transformer.ExportResult
import androidx.media3.transformer.Transformer
import kotlinx.coroutines.*
import org.json.JSONObject
import java.io.File
import java.nio.ByteOrder
import java.util.UUID
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlin.math.sqrt

object LocalAudio {
    fun metadata(context: Context, uri: Uri): JSONObject {
        val retriever = MediaMetadataRetriever()
        try {
            retriever.setDataSource(context, uri)
            var filename = "Your audio"
            context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
                if (it.moveToFirst()) filename = it.getString(0)
            }
            val duration = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toIntOrNull() ?: 0
            require(duration >= 1000) { "Choose an audio file at least one second long." }
            return JSONObject().put("id", "local-${UUID.randomUUID()}").put("status", "ready")
                .put("title", retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_TITLE) ?: filename)
                .put("artist", retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_ARTIST) ?: "Your audio")
                .put("durationMs", duration).put("localUri", uri.toString())
                .put("audio", JSONObject().put("url", uri.toString()))
        } finally { retriever.release() }
    }

    /** Decode real samples into 100 ms RMS buckets; no synthetic waveform. */
    suspend fun analyze(context: Context, uri: Uri): Pair<Int, FloatArray> = withContext(Dispatchers.IO) {
        val extractor = MediaExtractor()
        var codec: MediaCodec? = null
        try {
            extractor.setDataSource(context, uri, null)
            val track = (0 until extractor.trackCount).firstOrNull { extractor.getTrackFormat(it).getString(MediaFormat.KEY_MIME)?.startsWith("audio/") == true }
                ?: error("This file contains no supported audio.")
            extractor.selectTrack(track)
            val format = extractor.getTrackFormat(track)
            val duration = (format.getLong(MediaFormat.KEY_DURATION) / 1000).toInt()
            require(duration in 1000..7_200_000) { "Choose audio between one second and two hours long." }
            val sums = DoubleArray((duration + 99) / 100 + 1)
            val counts = IntArray(sums.size)
            var sampleRate = format.getInteger(MediaFormat.KEY_SAMPLE_RATE)
            var channels = format.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
            var floatPcm = false
            val decoder = MediaCodec.createDecoderByType(format.getString(MediaFormat.KEY_MIME)!!)
            codec = decoder
            decoder.configure(format, null, null, 0); decoder.start()
            var inputDone = false
            var outputDone = false
            val info = MediaCodec.BufferInfo()
            var lastOutput = android.os.SystemClock.elapsedRealtime()
            while (!outputDone) {
                ensureActive()
                check(android.os.SystemClock.elapsedRealtime() - lastOutput < 30_000) { "The audio decoder stopped responding. Try another file." }
                if (!inputDone) {
                    val index = decoder.dequeueInputBuffer(10_000)
                    if (index >= 0) {
                        val input = decoder.getInputBuffer(index)!!
                        val size = extractor.readSampleData(input, 0)
                        if (size < 0) {
                            decoder.queueInputBuffer(index, 0, 0, 0, MediaCodec.BUFFER_FLAG_END_OF_STREAM); inputDone = true
                        } else {
                            decoder.queueInputBuffer(index, 0, size, extractor.sampleTime, 0); extractor.advance()
                        }
                    }
                }
                val index = decoder.dequeueOutputBuffer(info, 10_000)
                if (index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                    val output = decoder.outputFormat
                    sampleRate = output.getInteger(MediaFormat.KEY_SAMPLE_RATE)
                    channels = output.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
                    val encoding = if (output.containsKey(MediaFormat.KEY_PCM_ENCODING)) output.getInteger(MediaFormat.KEY_PCM_ENCODING) else AudioFormat.ENCODING_PCM_16BIT
                    require(encoding == AudioFormat.ENCODING_PCM_16BIT || encoding == AudioFormat.ENCODING_PCM_FLOAT) { "Unsupported decoded audio format." }
                    floatPcm = encoding == AudioFormat.ENCODING_PCM_FLOAT
                } else if (index >= 0) {
                    lastOutput = android.os.SystemClock.elapsedRealtime()
                    val buffer = decoder.getOutputBuffer(index)!!.order(ByteOrder.nativeOrder())
                    buffer.position(info.offset); buffer.limit(info.offset + info.size)
                    val sampleBytes = if (floatPcm) 4 else 2
                    var sample = 0
                    while (buffer.remaining() >= sampleBytes) {
                        val value = if (floatPcm) buffer.float.toDouble() else buffer.short / 32768.0
                        val timeUs = info.presentationTimeUs + sample.toLong() * 1_000_000 / (sampleRate * channels)
                        val bucket = (timeUs / 100_000).toInt().coerceIn(0, sums.lastIndex)
                        sums[bucket] += value * value; counts[bucket]++; sample++
                    }
                    outputDone = info.flags and MediaCodec.BUFFER_FLAG_END_OF_STREAM != 0
                    decoder.releaseOutputBuffer(index, false)
                }
            }
            val levels = FloatArray(sums.size) { if (counts[it] == 0) 0f else sqrt(sums[it] / counts[it]).toFloat() }
            val peak = levels.maxOrNull()?.coerceAtLeast(.001f) ?: 1f
            duration to FloatArray(levels.size) { levels[it] / peak }
        } finally { codec?.release(); extractor.release() }
    }

    @androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
    suspend fun export(context: Context, uri: Uri, start: Int, duration: Int): File = withContext(Dispatchers.Main) {
        val directory = File(context.filesDir, "clips").apply { mkdirs() }
        val output = File(directory, "${UUID.randomUUID()}.m4a")
        suspendCancellableCoroutine { continuation ->
            val item = MediaItem.Builder().setUri(uri).setClippingConfiguration(
                MediaItem.ClippingConfiguration.Builder().setStartPositionMs(start.toLong())
                    .setEndPositionMs((start + duration).toLong()).build()).build()
            val edited = EditedMediaItem.Builder(item).setRemoveVideo(true).build()
            val transformer = Transformer.Builder(context).setAudioMimeType(MimeTypes.AUDIO_AAC)
                .addListener(object : Transformer.Listener {
                    override fun onCompleted(composition: Composition, result: ExportResult) {
                        if (continuation.isActive) continuation.resume(output)
                    }
                    override fun onError(composition: Composition, result: ExportResult, exception: ExportException) {
                        output.delete()
                        if (continuation.isActive) continuation.resumeWithException(exception)
                    }
                }).build()
            continuation.invokeOnCancellation {
                Handler(Looper.getMainLooper()).post { transformer.cancel(); output.delete() }
            }
            transformer.start(edited, output.absolutePath)
        }
    }
}
