package com.mifs.android

import android.content.Intent
import android.content.ActivityNotFoundException
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.media.MediaPlayer
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.*
import androidx.core.content.FileProvider
import kotlinx.coroutines.*
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

internal enum class Page { Discover, Recent, Editor, Playback }

class MainActivity : ComponentActivity() {
    private val scope = MainScope()
    private var screenJob: Job? = null
    private var playbackJob: Job? = null
    private var player: MediaPlayer? = null
    private var focus: AudioFocusRequest? = null
    private val prefs by lazy { getSharedPreferences("mifs", MODE_PRIVATE) }
    private val server get() = MusicServer(serverAddress)
    internal var serverAddress by mutableStateOf("https://mifs.cgn.fi")
    internal var appearance by mutableStateOf("system")
    internal var page by mutableStateOf(Page.Discover)
    internal var query by mutableStateOf("")
    internal var songs by mutableStateOf<List<JSONObject>>(emptyList())
    internal var recentMifs by mutableStateOf<List<JSONObject>>(emptyList())
    internal var song by mutableStateOf<JSONObject?>(null)
    internal var mif by mutableStateOf<JSONObject?>(null)
    internal var lyrics by mutableStateOf<List<JSONObject>>(emptyList())
    internal var waveform by mutableStateOf(FloatArray(0))
    internal var selection by mutableStateOf<Selection?>(null)
    internal var startMs by mutableIntStateOf(0)
    internal var lengthMs by mutableIntStateOf(10_000)
    internal var centerRequest by mutableIntStateOf(0)
    internal var loading by mutableStateOf(false)
    internal var loadingText by mutableStateOf("Finding songs…")
    internal var downloadProgress by mutableStateOf<Float?>(null)
    internal var screenError by mutableStateOf<String?>(null)
    internal var notice by mutableStateOf<String?>(null)
    internal var incomplete by mutableStateOf(false)
    internal var sharing by mutableStateOf(false)
    internal var sharePicker by mutableStateOf<JSONObject?>(null)
    internal var playback by mutableStateOf("idle")
    internal var playheadMs by mutableIntStateOf(0)
    internal var playbackProgress by mutableFloatStateOf(0f)
    private var origin = Page.Discover

    private val documentPicker = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) scope.launch {
            try {
                contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
                val metadata = withContext(Dispatchers.IO) { LocalAudio.metadata(this@MainActivity, uri) }
                openSong(metadata)
            } catch (error: Exception) { report(error) }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        serverAddress = prefs.getString("server", serverAddress)!!
        appearance = prefs.getString("appearance", "system")!!
        refreshRecent()
        setContent { MifsApp(this) }
        val savedSong = savedInstanceState?.getString("song")
        val savedMif = savedInstanceState?.getString("mif")
        when {
            savedMif != null -> openMif(JSONObject(savedMif), autoplay = false)
            savedSong != null -> openSong(JSONObject(savedSong), savedInstanceState.getInt("start"), savedInstanceState.getInt("length", 10_000))
            savedInstanceState?.getString("page") == Page.Recent.name -> navigate(Page.Recent)
            else -> { query = savedInstanceState?.getString("query").orEmpty(); loadSongs() }
        }
        if (savedInstanceState == null) handleIntent(intent)
    }
    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); handleIntent(intent) }
    private fun handleIntent(intent: Intent) {
        val text = intent.getStringExtra(Intent.EXTRA_TEXT) ?: return
        Regex("https?://[^\\s]+/m/[a-zA-Z0-9-]+").find(text)?.value?.let(::receive)
    }
    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString("page", page.name); outState.putString("query", query)
        if (page == Page.Editor) {
            outState.putString("song", song?.toString()); outState.putInt("start", startMs); outState.putInt("length", lengthMs)
        }
        if (page == Page.Playback) outState.putString("mif", mif?.toString())
    }
    override fun onStop() { stopPlayback(); super.onStop() }
    override fun onDestroy() { scope.cancel(); stopPlayback(); super.onDestroy() }

    internal fun navigate(destination: Page) {
        screenJob?.cancel(); stopPlayback(); page = destination; screenError = null; loading = false
        if (destination == Page.Discover) loadSongs() else if (destination == Page.Recent) refreshRecent()
    }
    internal fun back() = navigate(origin)
    internal fun search(value: String) { query = value; loadSongs(debounce = true) }
    internal fun loadSongs(debounce: Boolean = false) {
        screenJob?.cancel(); screenError = null; loading = true; incomplete = false
        val term = query.trim()
        screenJob = scope.launch {
            try {
                if (debounce) delay(320)
                val response = server.request(if (term.isEmpty()) "/v1/songs?limit=100" else "/v1/search?q=${Uri.encode(term)}&limit=30")
                songs = response.getJSONArray(if (term.isEmpty()) "songs" else "results").objects()
                incomplete = response.optBoolean("incomplete")
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                screenError = friendly(error); songs = emptyList()
            } finally { if (isActive) loading = false }
        }
    }
    internal fun openSong(initial: JSONObject, savedStart: Int? = null, savedLength: Int = 10_000) {
        if (page == Page.Discover || page == Page.Recent) origin = page
        screenJob?.cancel(); stopPlayback(); page = Page.Editor
        song = initial; mif = null; selection = null; waveform = FloatArray(0); lyrics = emptyList()
        startMs = savedStart ?: initial.optInt("highlightStartMs"); lengthMs = savedLength
        screenError = null; loading = true; loadingText = "Getting your song ready"; downloadProgress = null
        screenJob = scope.launch {
            try {
                var ready = initial.optJSONObject("song") ?: initial
                if (ready.has("ref")) ready = server.request("/v1/songs", JSONObject().put("ref", ready.getString("ref")))
                val deadline = android.os.SystemClock.elapsedRealtime() + 360_000
                while (ready.optString("status", "ready") != "ready") {
                    if (ready.optString("status") in listOf("failed", "unavailable")) error(ready.optString("statusMessage").ifBlank { "This song couldn't be prepared. Try another song." })
                    if (android.os.SystemClock.elapsedRealtime() >= deadline) error("The song is still preparing. Please try again in a minute.")
                    val total = ready.optLong("downloadTotalBytes")
                    downloadProgress = if (total > 0) (ready.optLong("downloadedBytes").toFloat() / total).coerceIn(0f, 1f) else null
                    loadingText = ready.optString("statusMessage").ifBlank {
                        when {
                            downloadProgress == 1f -> "Finishing your song"
                            total > 0 -> "Downloading your song"
                            ready.optString("status") == "pending" -> "Waiting to prepare your song"
                            else -> "Finding an audio source"
                        }
                    }
                    delay(750); ready = server.request("/v1/songs/${ready.getString("id")}")
                }
                song = ready
                val duration: Int
                if (ready.has("localUri")) {
                    loadingText = "Finding the shape of your song"
                    val analyzed = LocalAudio.analyze(this@MainActivity, Uri.parse(ready.getString("localUri")))
                    waveform = analyzed.second; duration = analyzed.first
                } else {
                    val data = server.request("/v1/songs/${ready.getString("id")}/waveform")
                    val values = data.getJSONArray("rms")
                    require(values.length() > 0) { "This song has no waveform yet." }
                    waveform = FloatArray(values.length()) { values.getDouble(it).toFloat() }
                    duration = data.getInt("durationMs")
                    lyrics = try { server.request("/v1/songs/${ready.getString("id")}/lyrics").getJSONArray("lines").objects() }
                    catch (error: ApiException) { if (error.status == 404) emptyList() else throw error }
                }
                selection = Selection(duration, savedStart ?: ready.optInt("highlightStartMs"), savedLength)
                syncRange(center = true)
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                screenError = friendly(error)
            } finally { if (isActive) loading = false }
        }
    }
    internal fun retrySong() { song?.let { openSong(it, startMs, lengthMs) } }
    internal fun syncRange(center: Boolean = false) {
        selection?.let { startMs = it.start; lengthMs = it.length }
        if (center) centerRequest++
    }
    internal fun moveRange(start: Int) { stopPlayback(); selection?.move(start); syncRange(true) }
    internal fun setLength(length: Int) { selection?.resize(length); syncRange(true); preview() }
    internal fun selectLyric(line: JSONObject) { selection?.move(maxOf(0, line.optInt("startMs") - 150)); syncRange(true); preview() }
    internal fun togglePreview() { if (playback != "idle") stopPlayback() else preview() }
    internal fun preview() {
        if (page == Page.Playback) {
            mif?.let {
                val url = if (it.has("localFile")) Uri.fromFile(File(filesDir, "clips/${it.getString("localFile")}")).toString()
                    else it.getJSONObject("audio").getString("url")
                play(url, 0, it.getInt("durationMs"), it.getInt("startMs"))
            }
        } else selection?.let { range -> song?.optJSONObject("audio")?.optString("url")?.let { play(it, range.start, range.length, range.start) } }
    }
    private fun play(url: String, start: Int, duration: Int, songOffset: Int) {
        stopPlayback(); playback = "loading"
        val manager = getSystemService(AUDIO_SERVICE) as AudioManager
        val attributes = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_MUSIC).build()
        focus = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT).setAudioAttributes(attributes)
            .setOnAudioFocusChangeListener { if (it < 0) stopPlayback() }.build()
        if (manager.requestAudioFocus(focus!!) != AudioManager.AUDIOFOCUS_REQUEST_GRANTED) {
            stopPlayback(); notice = "Another app is using audio. Please try again."; return
        }
        val media = MediaPlayer(); player = media
        fun begin(prepared: MediaPlayer) {
            if (player !== prepared) return
            prepared.start(); playback = "playing"
            playbackJob = scope.launch {
                while (player === prepared) {
                    val elapsed = (prepared.currentPosition - start).coerceAtLeast(0)
                    playheadMs = songOffset + elapsed
                    playbackProgress = (elapsed.toFloat() / duration).coerceIn(0f, 1f)
                    if (elapsed >= duration) { stopPlayback(); break }
                    delay(35)
                }
            }
        }
        try {
            media.setAudioAttributes(attributes); media.setDataSource(this, Uri.parse(url))
            media.setOnErrorListener { _, _, _ -> stopPlayback(); notice = "Couldn't play this audio. Check the server connection and try again."; true }
            media.setOnCompletionListener { stopPlayback() }
            media.setOnPreparedListener { if (player === it) { if (start == 0) begin(it) else it.seekTo(start.toLong(), MediaPlayer.SEEK_CLOSEST) } }
            media.setOnSeekCompleteListener { begin(it) }; media.prepareAsync()
        } catch (error: Exception) { stopPlayback(); report(error) }
    }
    internal fun stopPlayback() {
        playbackJob?.cancel(); playbackJob = null; player?.release(); player = null
        focus?.let { (getSystemService(AUDIO_SERVICE) as AudioManager).abandonAudioFocusRequest(it) }; focus = null
        playback = "idle"; playbackProgress = 0f
    }
    internal fun share() {
        if (sharing) return
        if (page == Page.Playback) { mif?.let { stopPlayback(); shareMif(it) }; return }
        val ready = song ?: return
        val range = selection ?: return
        val start = range.start; val length = range.length
        val levels = waveform
        val total = range.total
        sharing = true; stopPlayback()
        scope.launch {
            try {
                val created = if (ready.has("localUri")) {
                    val file = LocalAudio.export(this@MainActivity, Uri.parse(ready.getString("localUri")), start, length)
                    JSONObject().put("id", file.nameWithoutExtension).put("localFile", file.name).put("song", ready)
                        .put("startMs", start).put("durationMs", length)
                } else server.request("/v1/songs/${ready.getString("id")}/mifs", JSONObject().put("startMs", start).put("durationMs", length))
                // Keep the actual selected envelope for saved cards, including imported audio.
                val envelope = JSONArray()
                repeat(36) { i ->
                    val index = ((start + length * i / 36).toFloat() / total * levels.size).toInt().coerceIn(0, maxOf(0, levels.lastIndex))
                    envelope.put(if (levels.isEmpty()) 0.0 else levels[index].toDouble())
                }
                created.put("waveform", envelope); saveMif(created); shareMif(created)
            } catch (error: Exception) { report(error) }
            finally { sharing = false }
        }
    }
    private fun shareMif(value: JSONObject) {
        if (TelegramLink.startParameter(value.optString("url")) != null && !value.has("localFile")) {
            sharePicker = value
        } else shareOtherApps(value)
    }
    internal fun shareTelegram(value: JSONObject) {
        sharePicker = null
        val parameter = TelegramLink.startParameter(value.optString("url")) ?: return
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(TelegramLink.appURL(parameter))))
        } catch (_: ActivityNotFoundException) {
            try { startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(TelegramLink.webURL(parameter)))) }
            catch (error: ActivityNotFoundException) { report(error) }
        }
    }
    internal fun shareOtherApps(value: JSONObject) {
        sharePicker = null
        val ready = value.getJSONObject("song")
        val send = Intent(Intent.ACTION_SEND)
        if (value.has("localFile")) {
            val uri = FileProvider.getUriForFile(this, "$packageName.files", File(filesDir, "clips/${value.getString("localFile")}"))
            send.type = "audio/mp4"; send.putExtra(Intent.EXTRA_STREAM, uri)
            send.clipData = android.content.ClipData.newRawUri("MIFS clip", uri); send.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        } else {
            send.type = "text/plain"; send.putExtra(Intent.EXTRA_TEXT, "${ready.optString("title")} — ${ready.optString("artist")}\n${value.getString("url")}")
        }
        startActivity(Intent.createChooser(send, "Share your mif"))
    }
    private fun refreshRecent() {
        recentMifs = try { JSONArray(prefs.getString("recent", "[]")).objects() } catch (_: Exception) { emptyList() }
    }
    private fun saveMif(value: JSONObject) {
        recentMifs = (listOf(value) + recentMifs.filter { it.optString("id") != value.optString("id") }).take(100)
        prefs.edit().putString("recent", JSONArray(recentMifs).toString()).apply()
    }
    internal fun deleteMif(value: JSONObject) {
        recentMifs = recentMifs.filter { it.optString("id") != value.optString("id") }
        prefs.edit().putString("recent", JSONArray(recentMifs).toString()).apply(); navigate(Page.Recent)
    }
    internal fun receive(link: String) {
        val uri = Uri.parse(link); val configured = Uri.parse(serverAddress)
        if (uri.scheme !in listOf("http", "https") || uri.host != configured.host || uri.port != configured.port || uri.scheme != configured.scheme || uri.pathSegments.size != 2 || uri.pathSegments[0] != "m") {
            notice = "Open a mif link from your configured music server. You can change the server in Settings."; return
        }
        screenJob?.cancel(); loading = true
        screenJob = scope.launch {
            try { val value = server.request("/v1/mifs/${Uri.encode(uri.lastPathSegment)}"); saveMif(value); openMif(value) }
            catch (error: Exception) { report(error) }
            finally { loading = false }
        }
    }
    internal fun openMif(value: JSONObject, autoplay: Boolean = true) {
        origin = Page.Recent; screenJob?.cancel(); stopPlayback(); page = Page.Playback
        mif = value; song = value.getJSONObject("song"); lyrics = value.optJSONArray("lyrics")?.objects().orEmpty()
        startMs = value.optInt("startMs"); lengthMs = value.optInt("durationMs"); loading = false; screenError = null
        if (autoplay) preview()
    }
    internal fun saveSettings(address: String, theme: String): Boolean {
        val value = address.trim().trimEnd('/'); val uri = Uri.parse(value)
        if (uri.scheme !in listOf("http", "https") || uri.host.isNullOrBlank() || uri.userInfo != null || uri.query != null || uri.fragment != null) return false
        val changed = value != serverAddress
        serverAddress = value; appearance = theme
        prefs.edit().putString("server", value).putString("appearance", theme).apply()
        if (changed) navigate(Page.Discover)
        return true
    }
    internal fun importAudio() { documentPicker.launch(arrayOf("audio/*")) }
    private fun report(error: Exception) { if (error is CancellationException) throw error; notice = friendly(error) }
    private fun friendly(error: Exception) = if (error is java.io.IOException) "Couldn't reach the music server. Check your connection and the address in Settings." else error.message ?: "Something went wrong. Please try again."
}
