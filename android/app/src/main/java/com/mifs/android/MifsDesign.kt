@file:OptIn(androidx.compose.ui.text.ExperimentalTextApi::class)
package com.mifs.android

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.*
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.*
import androidx.compose.ui.unit.*
import org.json.JSONObject
import java.util.Locale

internal val Violet = Color(0xFF7255F5)
internal val Lilac = Color(0xFFBBAAFF)
internal val Pink = Color(0xFFF6A0BE)
internal val Night = Color(0xFF121019)
internal val BrandBrush = Brush.linearGradient(listOf(Violet, Color(0xFFA867DA), Pink))
private val Manrope = FontFamily(
    Font(R.font.manrope, FontWeight.Normal, variationSettings = FontVariation.Settings(FontVariation.weight(400))),
    Font(R.font.manrope, FontWeight.Medium, variationSettings = FontVariation.Settings(FontVariation.weight(500))),
    Font(R.font.manrope, FontWeight.SemiBold, variationSettings = FontVariation.Settings(FontVariation.weight(600))),
    Font(R.font.manrope, FontWeight.Bold, variationSettings = FontVariation.Settings(FontVariation.weight(700))),
    Font(R.font.manrope, FontWeight.ExtraBold, variationSettings = FontVariation.Settings(FontVariation.weight(800)))
)
private val AppTypography = Typography(
    displaySmall = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.ExtraBold, fontSize = 36.sp, letterSpacing = (-1.5).sp),
    headlineLarge = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.ExtraBold, fontSize = 32.sp, letterSpacing = (-1).sp),
    headlineMedium = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Bold, fontSize = 26.sp, lineHeight = 34.sp, letterSpacing = (-.6).sp),
    titleLarge = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Bold, fontSize = 21.sp, letterSpacing = (-.5).sp),
    titleMedium = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Bold, fontSize = 16.sp, lineHeight = 23.sp),
    titleSmall = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.SemiBold, fontSize = 14.sp),
    bodyLarge = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Medium, fontSize = 16.sp, lineHeight = 24.sp),
    bodyMedium = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Medium, fontSize = 14.sp, lineHeight = 21.sp),
    bodySmall = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Medium, fontSize = 12.sp, lineHeight = 18.sp),
    labelLarge = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Bold, fontSize = 14.sp),
    labelMedium = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.SemiBold, fontSize = 12.sp),
    labelSmall = TextStyle(fontFamily = Manrope, fontWeight = FontWeight.Bold, fontSize = 10.sp, letterSpacing = 1.4.sp)
)
@Composable
internal fun MifsTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val colors = if (dark) darkColorScheme(primary = Lilac, onPrimary = Night,
        background = Night, surface = Color(0xFF1C1924), surfaceContainer = Color(0xFF24202F),
        onBackground = Color(0xFFF8F5FF), onSurface = Color(0xFFF8F5FF), onSurfaceVariant = Color(0xFFADA7BD), outlineVariant = Color(0xFF35303F))
    else lightColorScheme(primary = Violet, onPrimary = Color.White,
        background = Color(0xFFFAF9FD), surface = Color.White, surfaceContainer = Color(0xFFF0EDF6),
        onBackground = Color(0xFF211D2B), onSurface = Color(0xFF211D2B), onSurfaceVariant = Color(0xFF817A90), outlineVariant = Color(0xFFEAE6F0))
    MaterialTheme(colorScheme = colors, typography = AppTypography, content = content)
}
internal fun timecode(ms: Int) = String.format(Locale.US, "%d:%02d", ms / 60_000, ms / 1000 % 60)
internal fun seconds(ms: Int) = if (ms % 1000 == 0) "${ms / 1000}s" else String.format(Locale.US, "%.1fs", ms / 1000.0)

@Composable
internal fun WaveMark(modifier: Modifier = Modifier, color: Color = Violet) {
    Canvas(modifier) {
        val bars = listOf(.27f, .55f, .85f, 1f, .64f, .4f, .22f)
        val step = size.width / bars.size
        bars.forEachIndexed { i, level ->
            drawLine(color, androidx.compose.ui.geometry.Offset(step * (i + .5f), size.height * (1 - level) / 2),
                androidx.compose.ui.geometry.Offset(step * (i + .5f), size.height * (1 + level) / 2), step * .52f, StrokeCap.Round)
        }
    }
}
@Composable
internal fun Cover(song: JSONObject, modifier: Modifier = Modifier, radius: Dp = 16.dp) {
    val artwork = rememberArtwork(song.artUrl())
    Box(modifier.clip(RoundedCornerShape(radius)).background(BrandBrush).border(.5.dp, Color.White.copy(alpha = .1f), RoundedCornerShape(radius)), contentAlignment = Alignment.Center) {
        if (artwork == null) WaveMark(Modifier.fillMaxSize(.4f), Color.White.copy(alpha = .7f))
        else Image(artwork.bitmap.asImageBitmap(), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
    }
}
@Composable
internal fun ArtBackdrop(song: JSONObject, modifier: Modifier = Modifier) {
    val art = rememberArtwork(song.artUrl())
    Box(modifier.background(art?.tint ?: Color(0xFF362B50))) {
        // A tiny filtered image produces an artwork wash on Android 11 as well as newer releases.
        art?.let { Image(it.wash.asImageBitmap(), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, alpha = .42f, filterQuality = FilterQuality.High) }
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color(0xFF171320).copy(alpha = .3f), Color(0xFF13101C).copy(alpha = .86f)))))
    }
}
@Composable
internal fun RoundAction(icon: ImageVector, description: String, modifier: Modifier = Modifier, dark: Boolean = false, onClick: () -> Unit) {
    IconButton(onClick, modifier.size(48.dp).clip(CircleShape).background(if (dark) Color.White.copy(alpha = .09f) else MaterialTheme.colorScheme.surface)) {
        Icon(icon, description, Modifier.size(21.dp), tint = if (dark) Color.White else MaterialTheme.colorScheme.onSurface)
    }
}
@Composable
internal fun Eyebrow(text: String, modifier: Modifier = Modifier, color: Color = MaterialTheme.colorScheme.onSurfaceVariant) {
    Text(text, modifier, style = MaterialTheme.typography.labelSmall, color = color)
}
@Composable
internal fun EmptyMessage(icon: ImageVector, title: String, subtitle: String, modifier: Modifier = Modifier, action: String? = null, onClick: () -> Unit = {}) {
    Column(modifier.fillMaxWidth().padding(28.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(72.dp).background(MaterialTheme.colorScheme.primary.copy(alpha = .09f), RoundedCornerShape(24.dp)), contentAlignment = Alignment.Center) {
            Icon(icon, null, Modifier.size(30.dp), tint = MaterialTheme.colorScheme.primary)
        }
        Text(title, style = MaterialTheme.typography.titleLarge, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        if (action != null) FilledTonalButton(onClick) { Text(action) }
    }
}
