@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
package com.mifs.android

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.automirrored.rounded.Send
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.*
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.*
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import org.json.JSONObject

@Composable
internal fun EditorScreen(app: MainActivity) {
    val song = app.song ?: return
    Box(Modifier.fillMaxSize()) {
        ArtBackdrop(song, Modifier.fillMaxSize())
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).padding(horizontal = 24.dp)) {
            PlayerHeader("MAKE A MIF", app::back)
            BoxWithConstraints(Modifier.weight(1f).fillMaxWidth()) {
                if (maxWidth > 600.dp) {
                    Row(Modifier.fillMaxSize().padding(top = 12.dp).testTag("editor-wide"), horizontalArrangement = Arrangement.spacedBy(32.dp)) {
                        Column(Modifier.weight(1f).fillMaxHeight()) {
                            CompactTrack(song)
                            EditorFocus(app, Modifier.weight(1f).fillMaxWidth())
                        }
                        Box(Modifier.width(340.dp).fillMaxHeight().verticalScroll(rememberScrollState()), contentAlignment = Alignment.Center) { EditorBottom(app, compact = true) }
                    }
                } else Column(Modifier.fillMaxSize().testTag("editor-portrait")) {
                    Spacer(Modifier.height(14.dp))
                    CompactTrack(song)
                    Spacer(Modifier.height(12.dp))
                    EditorFocus(app, Modifier.weight(1f).fillMaxWidth())
                    EditorBottom(app)
                }
            }
            Spacer(Modifier.height(14.dp))
        }
    }
}

@Composable
private fun PlayerHeader(label: String, back: () -> Unit, trailing: @Composable () -> Unit = { Spacer(Modifier.size(48.dp)) }) {
    Row(Modifier.fillMaxWidth().height(60.dp), verticalAlignment = Alignment.CenterVertically) {
        RoundAction(Icons.AutoMirrored.Rounded.ArrowBack, "Back", dark = true, onClick = back)
        Eyebrow(label, Modifier.weight(1f).wrapContentWidth(Alignment.CenterHorizontally), Color.White.copy(alpha = .7f))
        trailing()
    }
}

@Composable
private fun CompactTrack(song: JSONObject) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Cover(song, Modifier.size(56.dp), 15.dp)
        Column(Modifier.weight(1f).padding(start = 15.dp)) {
            Text(song.optString("title"), style = MaterialTheme.typography.titleMedium, color = Color.White, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(4.dp))
            Text(song.optString("artist"), style = MaterialTheme.typography.bodyMedium, color = Color.White.copy(alpha = .6f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        WaveMark(Modifier.padding(start = 16.dp).size(24.dp, 22.dp), Color.White.copy(alpha = .6f))
    }
}

@Composable
private fun EditorFocus(app: MainActivity, modifier: Modifier) {
    if (app.lyrics.isNotEmpty() && !app.loading) {
        Column(modifier.padding(top = 20.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(7.dp)) {
                Icon(Icons.Rounded.Notes, null, Modifier.size(15.dp), tint = Color.White.copy(alpha = .5f))
                Eyebrow("TAP A LINE. FIND YOUR MOMENT.", color = Color.White.copy(alpha = .5f))
            }
            LyricsFocus(app, Modifier.weight(1f), selectable = true)
        }
    } else Box(modifier.padding(vertical = 20.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Cover(app.song!!, Modifier.widthIn(max = 240.dp).fillMaxWidth(.6f).aspectRatio(1f), 28.dp)
            if (!app.loading) {
                Spacer(Modifier.height(18.dp))
                Text("Let the music do the talking.", style = MaterialTheme.typography.bodyMedium, color = Color.White.copy(alpha = .6f))
            }
        }
    }
}

@Composable
private fun LyricsFocus(app: MainActivity, modifier: Modifier, selectable: Boolean) {
    val state = rememberLazyListState()
    val haptics = LocalHapticFeedback.current
    val reference = if (app.playback == "playing") app.playheadMs else app.startMs
    val anchor = app.lyrics.indexOfFirst { it.optInt("endMs") > reference }.let { if (it < 0) maxOf(0, app.lyrics.lastIndex) else it }
    LaunchedEffect(anchor) { if (app.lyrics.isNotEmpty()) state.animateScrollToItem(maxOf(0, anchor - 1)) }
    LazyColumn(state = state, modifier = modifier.fillMaxWidth().graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
        .drawWithContent {
            drawContent()
            drawRect(Brush.verticalGradient(0f to Color.Transparent, .09f to Color.Black, .86f to Color.Black, 1f to Color.Transparent), blendMode = BlendMode.DstIn)
        }.testTag("lyrics"), contentPadding = PaddingValues(top = 32.dp, bottom = 46.dp), verticalArrangement = Arrangement.spacedBy(23.dp)) {
        itemsIndexed(app.lyrics) { index, line ->
            val chosen = !selectable || line.optInt("startMs") < app.startMs + app.lengthMs && line.optInt("endMs") > app.startMs
            val sung = app.playback == "playing" && index == anchor
            val alpha by animateFloatAsState(if (chosen) { if (app.playback != "playing" || sung) 1f else .7f } else .34f, label = "lyric emphasis")
            Text(line.optString("text"), style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.ExtraBold, fontSize = 25.sp, lineHeight = 35.sp),
                color = Color.White.copy(alpha = alpha), modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp))
                    .then(if (selectable) Modifier.clickable { haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove); app.selectLyric(line) } else Modifier)
                    .padding(vertical = 2.dp).semantics { selected = chosen })
        }
    }
}

@Composable
private fun EditorBottom(app: MainActivity, compact: Boolean = false) {
    when {
        app.screenError != null -> Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(Color.White.copy(alpha = .07f)).padding(22.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(Icons.Rounded.CloudOff, null, tint = Color.White)
            Text(app.screenError!!, style = MaterialTheme.typography.bodyMedium, color = Color.White.copy(alpha = .8f), textAlign = TextAlign.Center)
            FilledTonalButton(onClick = app::retrySong) { Text("Try again") }
        }
        app.loading -> Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(Color.White.copy(alpha = .07f)).padding(24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(app.loadingText, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = Color.White)
                app.downloadProgress?.let { Text("${(it * 100).toInt()}%", style = MaterialTheme.typography.labelMedium, color = Color.White) }
            }
            app.downloadProgress?.let { LinearProgressIndicator(progress = { it }, modifier = Modifier.fillMaxWidth().height(3.dp), color = Color.White, trackColor = Color.White.copy(alpha = .12f)) }
                ?: LinearProgressIndicator(modifier = Modifier.fillMaxWidth().height(3.dp), color = Color.White, trackColor = Color.White.copy(alpha = .12f))
            Text("The best part is almost here.", style = MaterialTheme.typography.bodySmall, color = Color.White.copy(alpha = .45f))
        }
        else -> {
            val range = app.selection ?: return
            val haptics = LocalHapticFeedback.current
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(if (compact) 8.dp else 16.dp)) {
                Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(26.dp)).background(Color.White.copy(alpha = .065f))
                    .border(.5.dp, Color.White.copy(alpha = .08f), RoundedCornerShape(26.dp)).padding(horizontal = 17.dp, vertical = if (compact) 10.dp else 15.dp)) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(timecode(app.startMs), Modifier.weight(1f), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = .65f))
                        Text("${seconds(app.lengthMs)} moment", style = MaterialTheme.typography.labelMedium, color = Color.White,
                            modifier = Modifier.clip(CircleShape).background(Color.White.copy(alpha = .09f)).padding(horizontal = 12.dp, vertical = 6.dp))
                        Text(timecode(app.startMs + app.lengthMs), Modifier.weight(1f), textAlign = TextAlign.End, style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = .65f))
                    }
                    Spacer(Modifier.height(8.dp))
                    AndroidView(factory = { context -> WaveformView(context, range, app.waveform).apply {
                        changed = { app.stopPlayback(); app.syncRange() }
                        finished = { app.syncRange(); app.preview() }
                    } }, modifier = Modifier.fillMaxWidth().height(if (compact) 52.dp else 84.dp).testTag("waveform").semantics {
                        customActions = listOf(
                            CustomAccessibilityAction("Shorten selection") { app.setLength(app.lengthMs - 1000); true },
                            CustomAccessibilityAction("Lengthen selection") { app.setLength(app.lengthMs + 1000); true })
                    }, update = { view ->
                        if (view.centerToken != app.centerRequest) { view.centerToken = app.centerRequest; view.post { view.center() } }
                        view.playhead = if (app.playback == "playing") app.playheadMs else null
                        view.invalidate()
                    })
                    Slider(value = app.startMs.toFloat(), onValueChange = { app.moveRange(it.toInt()) }, onValueChangeFinished = app::preview,
                        valueRange = 0f..maxOf(1, range.total - app.lengthMs).toFloat(), modifier = Modifier.fillMaxWidth().height(if (compact) 24.dp else 28.dp).semantics { contentDescription = "Song position" },
                        thumb = { Box(Modifier.size(10.dp).clip(CircleShape).background(Color.White)) },
                        track = { Canvas(Modifier.fillMaxWidth().height(3.dp)) {
                            val fraction = app.startMs.toFloat() / maxOf(1, range.total - app.lengthMs)
                            val y = size.height / 2
                            drawLine(Color.White.copy(alpha = .15f), androidx.compose.ui.geometry.Offset(0f, y), androidx.compose.ui.geometry.Offset(size.width, y), size.height, StrokeCap.Round)
                            drawLine(Color.White.copy(alpha = .8f), androidx.compose.ui.geometry.Offset(0f, y), androidx.compose.ui.geometry.Offset(size.width * fraction, y), size.height, StrokeCap.Round)
                        } },
                        colors = SliderDefaults.colors(thumbColor = Color.White, activeTrackColor = Color.White.copy(alpha = .8f), inactiveTrackColor = Color.White.copy(alpha = .12f)))
                    Text("Drag to explore · Pull the edges to trim", Modifier.fillMaxWidth(), style = MaterialTheme.typography.bodySmall.copy(fontSize = 10.sp), textAlign = TextAlign.Center, color = Color.White.copy(alpha = .46f))
                }
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(9.dp)) {
                    listOf(5, 10, 15, 20).forEach { seconds ->
                        val chosen = app.lengthMs == seconds * 1000
                        val color = if (chosen) Color.White else Color.White.copy(alpha = .08f)
                        Box(Modifier.weight(1f).height(if (compact) 36.dp else 44.dp).clip(CircleShape).background(color)
                            .clickable { haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove); app.setLength(seconds * 1000) }
                            .semantics { selected = chosen; contentDescription = "$seconds seconds" }.testTag("length-$seconds"), contentAlignment = Alignment.Center) {
                            Text("${seconds}s", style = MaterialTheme.typography.labelLarge, color = if (chosen) Night else Color.White.copy(alpha = .7f))
                        }
                    }
                }
                PlayerActions(app, compact)
            }
        }
    }
}

@Composable
private fun PlayerActions(app: MainActivity, compact: Boolean = false) {
    val haptics = LocalHapticFeedback.current
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(if (compact) 50.dp else 58.dp).clip(CircleShape).background(Color.White.copy(alpha = .12f))
            .clickable { haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove); app.togglePreview() }
            .testTag("preview").semantics { contentDescription = if (app.playback == "idle") "Play preview" else "Stop preview" }, contentAlignment = Alignment.Center) {
            if (app.playback == "loading") CircularProgressIndicator(Modifier.size(22.dp), color = Color.White, strokeWidth = 2.dp)
            else Icon(if (app.playback == "playing") Icons.Rounded.Stop else Icons.Rounded.PlayArrow, null, Modifier.size(27.dp), tint = Color.White)
            Canvas(Modifier.fillMaxSize().padding(1.5.dp)) {
                drawCircle(Color.White.copy(alpha = .12f), style = Stroke(1.dp.toPx()))
                if (app.playbackProgress > 0) drawArc(Color.White, -90f, app.playbackProgress * 360, false, style = Stroke(2.dp.toPx(), cap = StrokeCap.Round))
            }
        }
        Button(onClick = { haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove); app.share() }, enabled = !app.sharing,
            modifier = Modifier.weight(1f).height(if (compact) 50.dp else 58.dp).testTag("share"), colors = ButtonDefaults.buttonColors(containerColor = Color.White, contentColor = Night,
                disabledContainerColor = Color.White.copy(alpha = .65f), disabledContentColor = Night), shape = CircleShape) {
            if (app.sharing) CircularProgressIndicator(Modifier.size(18.dp), color = Night, strokeWidth = 2.dp)
            else Icon(Icons.AutoMirrored.Rounded.Send, null, Modifier.size(18.dp))
            Spacer(Modifier.width(10.dp)); Text(if (app.sharing) "Making your mif…" else "Share mif", style = MaterialTheme.typography.labelLarge)
        }
    }
}

@Composable
internal fun PlaybackScreen(app: MainActivity) {
    val song = app.song ?: return
    val mif = app.mif ?: return
    var menu by remember { mutableStateOf(false) }
    var delete by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxSize()) {
        ArtBackdrop(song, Modifier.fillMaxSize())
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).padding(horizontal = 24.dp)) {
            PlayerHeader("YOUR MIF", app::back) {
                Box {
                    RoundAction(Icons.Rounded.MoreHoriz, "More options", dark = true, onClick = { menu = true })
                    DropdownMenu(menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("Delete from Recent") }, onClick = { menu = false; delete = true }, leadingIcon = { Icon(Icons.Rounded.DeleteOutline, null) })
                    }
                }
            }
            Spacer(Modifier.height(20.dp))
            CompactTrack(song)
            Spacer(Modifier.height(24.dp))
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WaveMark(Modifier.size(20.dp, 16.dp), Color.White.copy(alpha = .65f))
                Eyebrow("${seconds(app.lengthMs)} OF A FEELING  ·  ${timecode(app.startMs)}", color = Color.White.copy(alpha = .6f))
            }
            if (app.lyrics.isNotEmpty()) LyricsFocus(app, Modifier.weight(1f), selectable = false)
            else Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
                Cover(song, Modifier.widthIn(max = 300.dp).fillMaxWidth(.85f).aspectRatio(1f), 32.dp)
            }
            LinearProgressIndicator(progress = { app.playbackProgress }, modifier = Modifier.fillMaxWidth().height(3.dp), color = Color.White, trackColor = Color.White.copy(alpha = .15f))
            Spacer(Modifier.height(12.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(timecode((app.playbackProgress * app.lengthMs).toInt()), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = .6f))
                Text(timecode(app.lengthMs), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = .6f))
            }
            Spacer(Modifier.height(22.dp))
            PlayerActions(app)
            TextButton(onClick = { app.openSong(song, mif.optInt("startMs"), mif.optInt("durationMs")) }, modifier = Modifier.align(Alignment.CenterHorizontally).padding(vertical = 10.dp)) {
                Icon(Icons.Rounded.ContentCut, null, Modifier.size(15.dp), tint = Color.White.copy(alpha = .6f)); Spacer(Modifier.width(8.dp))
                Text("Make another mif", style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = .6f))
            }
        }
        if (delete) AlertDialog(onDismissRequest = { delete = false }, title = { Text("Let this moment go?") }, text = { Text("It will be removed from Recent. Shared links will keep working.") },
            confirmButton = { TextButton(onClick = { delete = false; app.deleteMif(mif) }) { Text("Delete") } }, dismissButton = { TextButton(onClick = { delete = false }) { Text("Keep it") } })
    }
}
