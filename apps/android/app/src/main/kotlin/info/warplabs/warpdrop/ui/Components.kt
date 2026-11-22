@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui

import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.material3.carousel.HorizontalMultiBrowseCarousel
import androidx.compose.material3.carousel.rememberCarouselState
import android.net.Uri
import android.util.Size
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.IntOffset
import kotlinx.coroutines.launch
import kotlin.math.roundToInt
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.ui.theme.AvatarLetterFont
import info.warplabs.warpdrop.ui.theme.RoundedFontFamily

/**
 * One tab: a large flexible app bar over a scrolling column on the plain surface. [tabs] (a PrimaryTabRow with a
 * transparent container) sits under the title as part of the bar: it shares the bar's surface and stays pinned
 * when the title collapses, as the M3 tabs guidelines place them.
 */
@Composable
fun TabScaffold(
    title: String,
    navigationIcon: @Composable () -> Unit = {},
    floatingActionButton: (@Composable () -> Unit)? = null,
    snackbarHost: @Composable () -> Unit = {},
    tabs: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) = PageScaffold(title, navigationIcon, floatingActionButton, snackbarHost, tabs) { pad ->
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(pad)
            // with a FAB, the last row can scroll clear of it (56dp FAB + 16dp margin + 16dp gap)
            .padding(start = 16.dp, end = 16.dp, bottom = if (floatingActionButton != null) 88.dp else 16.dp),
        content = content,
    )
}

/** [TabScaffold] for a body that scrolls itself (a lazy list); [body] gets the app bar's padding. */
@Composable
fun PageScaffold(
    title: String,
    navigationIcon: @Composable () -> Unit = {},
    floatingActionButton: (@Composable () -> Unit)? = null,
    snackbarHost: @Composable () -> Unit = {},
    tabs: (@Composable () -> Unit)? = null,
    body: @Composable (PaddingValues) -> Unit,
) {
    val scroll = TopAppBarDefaults.exitUntilCollapsedScrollBehavior()
    val colors = TopAppBarDefaults.topAppBarColors()
    Scaffold(
        Modifier.nestedScroll(scroll.nestedScrollConnection),
        containerColor = MaterialTheme.colorScheme.surface,
        snackbarHost = snackbarHost,
        topBar = {
            // the bar tints as it collapses; the tab row under it follows the same colour so they read as one bar
            val bar = lerp(colors.containerColor, colors.scrolledContainerColor, scroll.state.collapsedFraction)
            Column(Modifier.background(bar)) {
                LargeFlexibleTopAppBar(
                    title = { Text(title, style = LocalTextStyle.current.copy(fontFamily = RoundedFontFamily), maxLines = 1) },
                    navigationIcon = navigationIcon,
                    scrollBehavior = scroll,
                    colors = colors,
                )
                tabs?.invoke()
            }
        },
        floatingActionButton = floatingActionButton ?: {},
        content = body,
    )
}

/** Back arrow for a page pushed on top of a tab. */
@Composable
fun BackButton(onBack: () -> Unit) = IconButton(onClick = onBack) { Glyph(R.drawable.ic_arrow_back, "Back") }

/** Group label above a block of rows, as in Google's own settings screens. */
@Composable
fun SectionHeader(title: String, action: (@Composable () -> Unit)? = null) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, top = 20.dp, bottom = 8.dp).heightIn(min = 24.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            title, Modifier.weight(1f),
            // Quick Share's group labels: rounded, medium weight, quiet colour
            style = MaterialTheme.typography.titleMedium.copy(fontFamily = RoundedFontFamily, fontWeight = FontWeight.Medium),
            color = MaterialTheme.colorScheme.onSurface,
        )
        action?.invoke()
    }
}

/** M3 defaults to surface rows on a tinted page; these pages are surface, so the rows take the tint. */
@Composable
fun rowColors() = ListItemDefaults.segmentedColors(
    containerColor = MaterialTheme.colorScheme.surfaceContainer,
    selectedContainerColor = MaterialTheme.colorScheme.secondaryContainer,
)

/** Rows of one group, 2dp apart, the first and last with the large outer corners. */
@Composable
fun RowGroup(content: @Composable ColumnScope.() -> Unit) =
    Column(verticalArrangement = Arrangement.spacedBy(ListItemDefaults.SegmentedGap), content = content)

/** A free-form block (QR code, request card) shaped like a one-row group. */
@Composable
fun SectionCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) = Column(
    modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(20.dp),
    content = content,
)

@Composable
fun Glyph(@DrawableRes icon: Int, contentDescription: String? = null, modifier: Modifier = Modifier, tint: Color = LocalContentColor.current) =
    Icon(painterResource(icon), contentDescription, modifier, tint)

/** An icon on a tinted expressive shape, for leading slots. */
@Composable
fun IconBadge(
    @DrawableRes icon: Int,
    container: Color = MaterialTheme.colorScheme.secondaryContainer,
    content: Color = MaterialTheme.colorScheme.onSecondaryContainer,
    size: Dp = 40.dp,
    shape: Shape = MaterialShapes.Cookie4Sided.toShape(),
) {
    Box(Modifier.size(size).background(container, shape), contentAlignment = Alignment.Center) {
        Glyph(icon, tint = content, modifier = Modifier.size(size * 0.55f))
    }
}

// The Material 3 Expressive shape set; a device always gets the same one, so it is recognisable across transfers.
private val deviceShapes = listOf(
    MaterialShapes.Cookie9Sided, MaterialShapes.Clover4Leaf, MaterialShapes.Sunny, MaterialShapes.Cookie6Sided,
    MaterialShapes.Pentagon, MaterialShapes.Gem, MaterialShapes.SoftBurst, MaterialShapes.Cookie7Sided,
)

/** A device's initial on its own expressive shape: how other devices appear. */
@Composable
fun DeviceAvatar(name: String, size: Dp = 40.dp) {
    val c = MaterialTheme.colorScheme
    val shape = deviceShapes[Math.floorMod(name.hashCode(), deviceShapes.size)].toShape()
    Box(Modifier.size(size).background(c.primaryContainer, shape), contentAlignment = Alignment.Center) {
        Text(
            name.trim().firstOrNull { it.isLetterOrDigit() }?.uppercase() ?: "?",
            color = c.onPrimaryContainer,
            style = TextStyle(fontFamily = AvatarLetterFont, fontSize = (size.value * 0.42f).sp),
        )
    }
}

/** Display-only expressive switch (the row handles the click) with a check or cross on the thumb. */
@Composable
fun IconSwitch(checked: Boolean) = Switch(
    checked, null,
    thumbContent = { Glyph(if (checked) R.drawable.ic_check else R.drawable.ic_close, modifier = Modifier.size(SwitchDefaults.IconSize)) },
)

/** Material Symbol for a MIME type, so a file list reads at a glance. */
@DrawableRes
fun fileIcon(mime: String, name: String): Int {
    val ext = name.substringAfterLast('.', "").lowercase()
    return when {
        mime.startsWith("image/") -> R.drawable.ic_image
        mime.startsWith("video/") -> R.drawable.ic_movie
        mime.startsWith("audio/") -> R.drawable.ic_music_note
        mime == "application/vnd.android.package-archive" || ext == "apk" -> R.drawable.ic_android
        ext in setOf("zip", "rar", "7z", "tar", "gz", "tgz", "xz") -> R.drawable.ic_folder_zip
        mime.startsWith("text/") || mime == "application/pdf" || ext in setOf("pdf", "doc", "docx", "odt", "md") -> R.drawable.ic_description
        else -> R.drawable.ic_draft
    }
}

/** "Direct" or "Relay": how the bytes travel, same labels as the web app. */
fun transportLabel(transport: String) = when (transport) {
    "rtc" -> "Direct"
    "relay" -> "Relay"
    "lan" -> "Local network"
    else -> ""
}

/** "Pixel 8 on Android" -> Pixel 8 / Android, "box (CLI 1.2)" -> box / CLI 1.2: the web app's split of a device label. */
fun deviceTitle(name: String): Pair<String, String> {
    val m = Regex(" on ([^()]+)$").find(name) ?: Regex(" \\((CLI[^)]*)\\)$").find(name)
    return if (m == null) name to "" else name.substring(0, m.range.first) to m.groupValues[1]
}

/** The 6-digit nearby code to compare with the other screen, in two groups so it reads aloud: 482 193. */
@Composable
fun SasCode(sas: String, who: String) = Column {
    Text("Check this code with $who", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
    Text(sas.chunked(3).joinToString(" "), style = MaterialTheme.typography.displaySmallEmphasized, modifier = Modifier.padding(vertical = 4.dp))
    Text(
        "It must match their screen. If it doesn't, decline: someone may be intercepting the connection.",
        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** Joins the non-empty parts with " · ". */
fun dotted(vararg parts: String) = parts.filter { it.isNotEmpty() }.joinToString(" · ")

@Composable
fun ErrorRow(message: String) = SegmentedListItem(
    shapes = ListItemDefaults.segmentedShapes(0, 1),
    colors = ListItemDefaults.segmentedColors(
        containerColor = MaterialTheme.colorScheme.errorContainer,
        contentColor = MaterialTheme.colorScheme.onErrorContainer,
        leadingContentColor = MaterialTheme.colorScheme.onErrorContainer,
    ),
    leadingContent = { Glyph(R.drawable.ic_error) },
) { Text(message, maxLines = 4, overflow = TextOverflow.Ellipsis) }

fun isMedia(mime: String) = mime.startsWith("image/") || mime.startsWith("video/")

/** A platform-made thumbnail (photos and videos), loaded off the main thread; null until ready or when there is none. */
@Composable
fun rememberThumbnail(uri: String?, px: Int = 480): ImageBitmap? {
    val resolver = LocalContext.current.contentResolver
    return produceState<ImageBitmap?>(null, uri) {
        value = uri?.let { u ->
            withContext(Dispatchers.IO) {
                runCatching { resolver.loadThumbnail(Uri.parse(u), Size(px, px), null).asImageBitmap() }.getOrNull()
            }
        }
    }.value
}

/** A photo or video in a carousel; [uri] is null until the file exists on this device. */
class Media(val key: Any, val name: String, val mime: String, val uri: String?)

/**
 * M3 multi-browse carousel of photo and video thumbnails, like the image tiles on the web app.
 * Tapping a tile brings it to the front. The remove button rides the visible (masked) edge of the tile and only
 * appears once the tile is mostly open, so you never remove a photo you can't see.
 */
@Composable
fun MediaCarousel(items: List<Media>, onRemove: ((Media) -> Unit)? = null) {
    val state = rememberCarouselState { items.size }
    val scope = rememberCoroutineScope()
    HorizontalMultiBrowseCarousel(
        state = state,
        preferredItemWidth = 176.dp,
        itemSpacing = 8.dp,
        modifier = Modifier.fillMaxWidth().height(176.dp),
    ) { i ->
        val m = items[i]
        val thumb = rememberThumbnail(m.uri)
        val info = carouselItemDrawInfo
        // 0 when squeezed to the smallest tile, 1 when fully open; read in layout/draw so scrolling doesn't recompose
        val open = { if (info.maxSize > info.minSize) ((info.size - info.minSize) / (info.maxSize - info.minSize)).coerceIn(0f, 1f) else 1f }
        Box(Modifier.fillMaxSize()) {
            Box(
                Modifier.fillMaxSize().maskClip(MaterialTheme.shapes.extraLarge).background(MaterialTheme.colorScheme.surfaceContainerHigh)
                    .clickable(onClickLabel = "Show ${m.name}") { scope.launch { state.animateScrollToItem(i) } },
            ) {
                if (thumb != null) Image(thumb, m.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                else Glyph(fileIcon(m.mime, m.name), m.name, Modifier.align(Alignment.Center).size(32.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (m.mime.startsWith("video/") && thumb != null) Glyph(
                R.drawable.ic_movie, null,
                Modifier.offset { IntOffset(info.maskRect.left.roundToInt() + 10.dp.roundToPx(), 0) }
                    .align(Alignment.BottomStart).padding(bottom = 10.dp).size(20.dp),
                tint = Color.White,
            )
            if (onRemove != null) {
                val shown by remember { derivedStateOf { open() > 0.6f } }
                FilledTonalIconButton(
                    onClick = { onRemove(m) },
                    enabled = shown,
                    colors = IconButtonDefaults.filledTonalIconButtonColors(
                        disabledContainerColor = MaterialTheme.colorScheme.secondaryContainer,
                        disabledContentColor = MaterialTheme.colorScheme.onSecondaryContainer,
                    ),
                    modifier = Modifier
                        .offset { IntOffset(info.maskRect.right.roundToInt() - 38.dp.roundToPx(), info.maskRect.top.roundToInt() + 6.dp.roundToPx()) }
                        .graphicsLayer { alpha = ((open() - 0.4f) / 0.5f).coerceIn(0f, 1f) }
                        .size(32.dp),
                ) { Glyph(R.drawable.ic_close, "Remove ${m.name}", Modifier.size(18.dp)) }
            }
        }
    }
}

/** Squiggly determinate ring for a file that is on its way, in the row's leading slot; waves scaled down for 32dp. */
@Composable
fun FileProgress(fraction: Float) = CircularWavyProgressIndicator(
    progress = { fraction },
    modifier = Modifier.size(32.dp),
    trackColor = MaterialTheme.colorScheme.surfaceContainerHighest,
    stroke = Stroke(with(LocalDensity.current) { 3.dp.toPx() }, cap = StrokeCap.Round),
    trackStroke = Stroke(with(LocalDensity.current) { 3.dp.toPx() }, cap = StrokeCap.Round),
    wavelength = 8.dp,
)
