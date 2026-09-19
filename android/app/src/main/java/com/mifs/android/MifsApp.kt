@file:OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class, androidx.compose.ui.ExperimentalComposeUiApi::class)
package com.mifs.android

import androidx.activity.compose.BackHandler
import androidx.compose.animation.*
import androidx.compose.animation.core.tween
import androidx.compose.foundation.*
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.*
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.*
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowCompat
import org.json.JSONObject
import kotlin.math.sqrt

@Composable
internal fun MifsApp(app: MainActivity) {
    val immersive = app.page == Page.Editor || app.page == Page.Playback
    val dark = immersive || app.appearance == "dark" || (app.appearance == "system" && isSystemInDarkTheme())
    val view = LocalView.current
    SideEffect {
        WindowCompat.getInsetsController(app.window, view).apply {
            isAppearanceLightStatusBars = !dark; isAppearanceLightNavigationBars = !dark
        }
        if (android.os.Build.VERSION.SDK_INT >= 29) app.window.isNavigationBarContrastEnforced = false
    }
    var settings by rememberSaveable { mutableStateOf(false) }
    var openLink by rememberSaveable { mutableStateOf(false) }
    BackHandler(immersive && !app.sharing) { app.back() }
    MifsTheme(dark) {
        Surface(Modifier.fillMaxSize().semantics { testTagsAsResourceId = true }, color = MaterialTheme.colorScheme.background) {
            AnimatedContent(app.page, transitionSpec = {
                (fadeIn(tween(240)) + slideInHorizontally(tween(280)) { if (targetState == Page.Editor || targetState == Page.Playback) it / 12 else 0 }) togetherWith fadeOut(tween(120))
            }, label = "screen") { page ->
                when (page) {
                    Page.Discover, Page.Recent -> BrowseShell(app, page, { settings = true }, { openLink = true })
                    Page.Editor -> EditorScreen(app)
                    Page.Playback -> PlaybackScreen(app)
                }
            }
            if (settings) SettingsSheet(app) { settings = false }
            if (openLink) LinkSheet(app) { openLink = false }
            app.notice?.let { message ->
                AlertDialog(onDismissRequest = { app.notice = null }, icon = { Icon(Icons.Rounded.Info, null) },
                    title = { Text("A quick note") }, text = { Text(message) },
                    confirmButton = { TextButton(onClick = { app.notice = null }) { Text("Got it") } })
            }
        }
    }
}

@Composable
private fun BrowseShell(app: MainActivity, page: Page, settings: () -> Unit, openLink: () -> Unit) {
    Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Top + WindowInsetsSides.Horizontal))) {
        Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.TopCenter) {
            if (page == Page.Discover) Discover(app, settings) else Recent(app, settings, openLink)
        }
        Box(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surface).navigationBarsPadding(), contentAlignment = Alignment.Center) {
            Row(Modifier.widthIn(max = 520.dp).fillMaxWidth().padding(horizontal = 28.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                listOf(Page.Discover to Icons.Rounded.Explore, Page.Recent to Icons.Rounded.GraphicEq).forEach { (destination, icon) ->
                    val selected = destination == page
                    val fill by animateColorAsState(if (selected) MaterialTheme.colorScheme.primary.copy(alpha = .1f) else Color.Transparent, label = "tab")
                    Row(Modifier.weight(1f).height(54.dp).clip(RoundedCornerShape(20.dp)).background(fill)
                        .clickable(role = Role.Tab) { app.navigate(destination) }
                        .testTag("nav-${destination.name.lowercase()}").semantics { this.selected = selected },
                        horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                        Icon(icon, null, Modifier.size(22.dp), tint = if (selected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant)
                        Spacer(Modifier.width(8.dp))
                        Text(destination.name, style = MaterialTheme.typography.labelLarge, color = if (selected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}

@Composable
private fun BrowseHeader(title: String, subtitle: String, settings: () -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(title, style = MaterialTheme.typography.displaySmall)
                if (title == "MIFS") WaveMark(Modifier.size(28.dp, 24.dp))
            }
            Spacer(Modifier.height(6.dp))
            Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        RoundAction(Icons.Rounded.Tune, "Settings", onClick = settings)
    }
}

@Composable
private fun Discover(app: MainActivity, settings: () -> Unit) {
    val keyboard = LocalSoftwareKeyboardController.current
    val focus = LocalFocusManager.current
    LazyColumn(Modifier.widthIn(max = 720.dp).fillMaxSize().testTag("discover-list"), contentPadding = PaddingValues(start = 24.dp, end = 24.dp, top = 18.dp, bottom = 20.dp)) {
        item {
            BrowseHeader("MIFS", "Find a song. Keep the feeling.", settings)
            Spacer(Modifier.height(26.dp))
            TextField(value = app.query, onValueChange = app::search, modifier = Modifier.fillMaxWidth().testTag("song-search"),
                placeholder = { Text("Songs, artists, a feeling…", style = MaterialTheme.typography.bodyMedium) },
                leadingIcon = { Icon(Icons.Rounded.Search, null, Modifier.size(22.dp)) },
                trailingIcon = if (app.query.isNotEmpty()) { { IconButton(onClick = { app.search("") }) { Icon(Icons.Rounded.Close, "Clear search", Modifier.size(18.dp)) } } } else null,
                shape = RoundedCornerShape(20.dp), singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { keyboard?.hide(); focus.clearFocus(); app.loadSongs() }),
                colors = TextFieldDefaults.colors(focusedContainerColor = MaterialTheme.colorScheme.surfaceContainer, unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainer,
                    focusedIndicatorColor = Color.Transparent, unfocusedIndicatorColor = Color.Transparent))
            Spacer(Modifier.height(26.dp))
        }
        if (app.query.isBlank() && !app.loading && app.songs.isNotEmpty() && app.screenError == null) {
            item {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Eyebrow("READY FOR YOUR NEXT MIF", Modifier.weight(1f))
                    Text("01 — ${app.songs.take(3).size.toString().padStart(2, '0')}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Spacer(Modifier.height(14.dp))
                LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    items(app.songs.take(3), key = { it.optString("id") }) { song -> FeaturedSong(song) { focus.clearFocus(); app.openSong(song) } }
                }
                Spacer(Modifier.height(28.dp))
            }
        }
        item {
            Row(Modifier.fillMaxWidth().padding(bottom = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(if (app.query.isBlank()) "Top Songs" else "Search results", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                if (app.query.isBlank()) TextButton(onClick = app::importAudio, contentPadding = PaddingValues(horizontal = 10.dp)) {
                    Icon(Icons.Rounded.Add, null, Modifier.size(17.dp)); Spacer(Modifier.width(4.dp)); Text("Your audio", style = MaterialTheme.typography.labelMedium)
                } else Eyebrow("SPOTIFY", color = MaterialTheme.colorScheme.primary)
            }
        }
        when {
            app.loading -> items(5) { SkeletonSong() }
            app.screenError != null -> item { EmptyMessage(Icons.Rounded.CloudOff, "Let's reconnect", app.screenError!!, action = "Try again", onClick = { app.loadSongs() }) }
            app.songs.isEmpty() -> item { EmptyMessage(Icons.Rounded.MusicNote, "Find your first moment", if (app.query.isBlank()) "Search for a song or bring your own audio." else "Try a different song or artist.", action = if (app.query.isBlank()) "Import audio" else null, onClick = app::importAudio) }
            else -> {
                if (app.incomplete) item { Text("Some results are taking a little longer. Try again in a moment.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                itemsIndexed(app.songs, key = { _, it -> it.optString("ref", it.optString("id")) }) { index, song ->
                    SongRow(song, if (app.query.isBlank()) index + 1 else null) { keyboard?.hide(); focus.clearFocus(); app.openSong(song) }
                }
            }
        }
        item {
            if (!app.loading && app.songs.isNotEmpty()) {
                Spacer(Modifier.height(20.dp))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                    WaveMark(Modifier.size(16.dp, 14.dp), MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = .5f))
                    Spacer(Modifier.width(8.dp)); Text("Small moments. Big feelings.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

@Composable
private fun FeaturedSong(song: JSONObject, onClick: () -> Unit) {
    val art = rememberArtwork(song.artUrl())
    Box(Modifier.width(148.dp).height(184.dp).clip(RoundedCornerShape(24.dp)).background(art?.tint ?: Violet).clickable(onClick = onClick)) {
        Cover(song, Modifier.fillMaxSize(), 24.dp)
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = .12f), Color.Black.copy(alpha = .83f)))))
        Box(Modifier.padding(12.dp).size(28.dp).clip(CircleShape).background(Color.White.copy(alpha = .18f)).align(Alignment.TopEnd), contentAlignment = Alignment.Center) {
            Icon(Icons.Rounded.NorthEast, null, Modifier.size(16.dp), tint = Color.White)
        }
        Column(Modifier.align(Alignment.BottomStart).padding(14.dp)) {
            Text(song.optString("title"), style = MaterialTheme.typography.titleSmall, color = Color.White, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(3.dp))
            Text(song.optString("artist"), style = MaterialTheme.typography.bodySmall, color = Color.White.copy(alpha = .7f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun SongRow(song: JSONObject, rank: Int?, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).clickable(onClick = onClick)
        .testTag("song-${song.optString("id", song.optString("ref"))}").padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        if (rank != null) Text(rank.toString().padStart(2, '0'), Modifier.width(29.dp), color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = .6f), style = MaterialTheme.typography.labelMedium)
        Cover(song, Modifier.size(54.dp), 13.dp)
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(song.optString("title"), style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(4.dp))
            Text(song.optString("artist"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Spacer(Modifier.width(12.dp))
        Box(Modifier.size(34.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primary.copy(alpha = .07f)), contentAlignment = Alignment.Center) {
            Icon(if (song.optString("status") == "ready") Icons.Rounded.ChevronRight else Icons.Rounded.Add, null, Modifier.size(19.dp), tint = MaterialTheme.colorScheme.primary)
        }
    }
}

@Composable
private fun SkeletonSong() {
    Row(Modifier.fillMaxWidth().padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(54.dp).clip(RoundedCornerShape(13.dp)).background(MaterialTheme.colorScheme.surfaceContainer))
        Column(Modifier.padding(start = 14.dp), verticalArrangement = Arrangement.spacedBy(9.dp)) {
            Box(Modifier.size(150.dp, 12.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceContainer))
            Box(Modifier.size(94.dp, 9.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceContainer))
        }
    }
}

@Composable
private fun Recent(app: MainActivity, settings: () -> Unit, openLink: () -> Unit) {
    LazyColumn(Modifier.widthIn(max = 720.dp).fillMaxSize(), contentPadding = PaddingValues(24.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        item { BrowseHeader("Recent", "Some moments are worth keeping.", settings); Spacer(Modifier.height(12.dp)) }
        item {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Eyebrow("${app.recentMifs.size} SAVED MOMENTS", Modifier.weight(1f))
                TextButton(onClick = openLink) { Icon(Icons.Rounded.Link, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Open a link", style = MaterialTheme.typography.labelMedium) }
            }
        }
        if (app.recentMifs.isEmpty()) item {
            EmptyMessage(Icons.Rounded.GraphicEq, "Your little collection", "The moments you share will live here. Ready to find your first one?", action = "Discover a song", onClick = { app.navigate(Page.Discover) })
        }
        items(app.recentMifs, key = { it.optString("id") }) { mif -> RecentCard(mif) { app.openMif(mif) } }
    }
}

@Composable
private fun RecentCard(mif: JSONObject, onClick: () -> Unit) {
    val song = mif.getJSONObject("song")
    val art = rememberArtwork(song.artUrl())
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(26.dp)).background(Brush.linearGradient(listOf(art?.tint ?: Color(0xFF433658), Color(0xFF211B30))))
        .clickable(onClick = onClick).testTag("mif-${song.optString("id")}").padding(20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Cover(song, Modifier.size(56.dp), 14.dp)
            Column(Modifier.weight(1f).padding(horizontal = 14.dp)) {
                Text(song.optString("title"), style = MaterialTheme.typography.titleMedium, color = Color.White, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(4.dp)); Text(song.optString("artist"), style = MaterialTheme.typography.bodySmall, color = Color.White.copy(alpha = .65f))
            }
            Box(Modifier.size(42.dp).clip(CircleShape).background(Color.White.copy(alpha = .13f)), contentAlignment = Alignment.Center) {
                Icon(Icons.Rounded.PlayArrow, "Play saved mif", Modifier.size(24.dp), tint = Color.White)
            }
        }
        Spacer(Modifier.height(22.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            val values = mif.optJSONArray("waveform")
            if (values != null && values.length() > 0) Canvas(Modifier.weight(1f).height(24.dp)) {
                val step = size.width / values.length()
                repeat(values.length()) { i ->
                    val height = (sqrt(values.optDouble(i).coerceIn(0.0, 1.0)).toFloat() * size.height).coerceAtLeast(3.dp.toPx())
                    drawLine(Color.White.copy(alpha = .7f), androidx.compose.ui.geometry.Offset(step * (i + .5f), (size.height-height)/2), androidx.compose.ui.geometry.Offset(step*(i+.5f), (size.height+height)/2), step*.45f, StrokeCap.Round)
                }
            } else Row(Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
                WaveMark(Modifier.size(32.dp, 22.dp), Color.White.copy(alpha = .7f)); Spacer(Modifier.width(10.dp)); Text("A moment in music", style = MaterialTheme.typography.bodySmall, color = Color.White.copy(alpha = .65f))
            }
            Spacer(Modifier.width(20.dp)); Text(seconds(mif.optInt("durationMs")), style = MaterialTheme.typography.labelLarge, color = Color.White)
        }
        Spacer(Modifier.height(12.dp))
        Text("${timecode(mif.optInt("startMs"))} — ${timecode(mif.optInt("startMs") + mif.optInt("durationMs"))}", style = MaterialTheme.typography.bodySmall, color = Color.White.copy(alpha = .48f))
    }
}

@Composable
private fun SettingsSheet(app: MainActivity, dismiss: () -> Unit) {
    var address by remember { mutableStateOf(app.serverAddress) }
    var appearance by remember { mutableStateOf(app.appearance) }
    var invalid by remember { mutableStateOf(false) }
    ModalBottomSheet(onDismissRequest = dismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true), containerColor = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).imePadding().padding(horizontal = 26.dp).padding(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            Text("Make yourself at home", style = MaterialTheme.typography.headlineMedium)
            Eyebrow("APPEARANCE")
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                listOf("system", "light", "dark").forEachIndexed { index, option ->
                    SegmentedButton(selected = appearance == option, onClick = { appearance = option }, shape = SegmentedButtonDefaults.itemShape(index, 3)) { Text(option.replaceFirstChar { it.uppercase() }, style = MaterialTheme.typography.labelMedium) }
                }
            }
            Spacer(Modifier.height(4.dp)); Eyebrow("MUSIC SERVER")
            OutlinedTextField(address, { address = it; invalid = false }, Modifier.fillMaxWidth(), shape = RoundedCornerShape(18.dp), singleLine = true, isError = invalid,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), label = { Text("Server address") },
                supportingText = { Text(if (invalid) "Enter a valid HTTP or HTTPS address." else "Use a public server to share playable links with friends.") })
            Button(onClick = { if (app.saveSettings(address, appearance)) dismiss() else invalid = true }, modifier = Modifier.fillMaxWidth().height(54.dp), shape = CircleShape) { Text("Save changes") }
            Text("MIFS  ·  Made for the moments", Modifier.align(Alignment.CenterHorizontally), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun LinkSheet(app: MainActivity, dismiss: () -> Unit) {
    var link by remember { mutableStateOf("") }
    ModalBottomSheet(onDismissRequest = dismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(24.dp).imePadding(), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            Text("Someone sent you a feeling", style = MaterialTheme.typography.headlineMedium)
            Text("Paste their mif link to listen.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedTextField(link, { link = it }, Modifier.fillMaxWidth(), placeholder = { Text("https://…/m/…") }, shape = RoundedCornerShape(18.dp), singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri))
            Button(onClick = { dismiss(); app.receive(link.trim()) }, enabled = link.isNotBlank(), modifier = Modifier.fillMaxWidth().height(54.dp)) { Text("Open moment"); Spacer(Modifier.width(8.dp)); Icon(Icons.AutoMirrored.Rounded.ArrowForward, null, Modifier.size(18.dp)) }
        }
    }
}
