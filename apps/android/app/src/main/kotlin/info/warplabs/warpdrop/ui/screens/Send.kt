@file:OptIn(ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui.screens

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.layout.*
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import info.warplabs.warpdrop.AndroidFile
import info.warplabs.warpdrop.AppController
import info.warplabs.warpdrop.Platform
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.core.*
import info.warplabs.warpdrop.ui.*
import io.github.alexzhirkevich.qrose.rememberQrCodePainter
import kotlinx.coroutines.launch

@Composable
fun SendTab(controller: AppController, platform: Platform) {
    val session by controller.send.collectAsState()
    val s = session
    if (s != null) SharingPage(s, platform, controller::closeSend)
    else PickPage(controller, platform)
}

private enum class SendMode(val label: String, val icon: Int) { Nearby("Nearby", R.drawable.ic_smartphone), Code("Code", R.drawable.ic_link) }

@Composable
private fun PickPage(controller: AppController, platform: Platform) {
    val picked by controller.picked.collectAsState()
    val nearby by controller.nearbyState.collectAsState()
    val sends by controller.nearbySends.collectAsState()
    val sending by controller.sendingNearby.collectAsState()
    var mode by rememberSaveable { mutableStateOf(SendMode.Nearby) }
    val snack = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    // Removing is one tap, so it is undoable, like deleting in Google Photos. Restored files go back at the end.
    fun remove(gone: List<PickedFile>, message: String) {
        gone.forEach(controller::removeFile)
        scope.launch {
            snack.currentSnackbarData?.dismiss()
            if (snack.showSnackbar(message, "Undo", duration = SnackbarDuration.Short) == SnackbarResult.ActionPerformed) controller.addFiles(gone)
        }
    }
    TabScaffold(
        "Send",
        snackbarHost = { SnackbarHost(snack) },
        tabs = {
            PrimaryTabRow(selectedTabIndex = mode.ordinal, containerColor = Color.Transparent) {
                SendMode.entries.forEach { m ->
                    Tab(
                        selected = mode == m,
                        onClick = { mode = m },
                        text = { Text(m.label) },
                        icon = { Glyph(m.icon) },
                        unselectedContentColor = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
    ) {
        SectionHeader("You'll appear as")
        ThisDevice(
            controller.deviceName,
            when {
                mode == SendMode.Code -> "Shown to whoever enters your code"
                nearby.visible -> "Visible to devices on this network"
                else -> "Hidden, but you can still send"
            },
        )

        SectionHeader(
            if (picked.isEmpty()) "Selected files" else dotted(filesLabel(picked.size), formatBytes(picked.sumOf { it.size })),
            // files stay put while devices are receiving them, like Quick Share's greyed "Clear files"
            action = if (picked.isEmpty()) null else ({
                TextButton({ remove(picked, "Removed ${filesLabel(picked.size)}") }, enabled = !sending) { Text("Clear") }
            }),
        )
        val (media, docs) = picked.partition { isMedia(it.mime) }
        if (media.isNotEmpty()) {
            MediaCarousel(media.map { it.asMedia() }) { if (!sending) remove(listOf(it.key as PickedFile), "Removed ${it.name}") }
            Spacer(Modifier.height(ListItemDefaults.SegmentedGap * 4))
        }
        RowGroup {
            val n = docs.size + 1
            docs.forEachIndexed { i, f ->
                FileItem(f.name, formatBytes(f.size), fileIcon(f.mime, f.name), i, n) {
                    IconButton(onClick = { remove(listOf(f), "Removed ${f.name}") }, enabled = !sending) { Glyph(R.drawable.ic_close, "Remove ${f.name}") }
                }
            }
            SegmentedListItem(
                onClick = platform::pickFiles,
                shapes = ListItemDefaults.segmentedShapes(n - 1, n),
                colors = rowColors(),
                leadingContent = {
                    IconBadge(R.drawable.ic_add, MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer)
                },
                supportingContent = if (picked.isEmpty()) ({ Text("Photos, videos, documents. Any size.") }) else null,
            ) { Text(if (picked.isEmpty()) "Choose files" else "Add more files") }
        }

        AnimatedContent(mode, transitionSpec = { fadeIn(tween(200)) togetherWith fadeOut(tween(120)) }, label = "mode") { m ->
            Column {
                when (m) {
                    SendMode.Nearby -> {
                        SectionHeader("Send to nearby devices")
                        NearbyCard(
                            nearby, sends, ready = picked.isNotEmpty(),
                            onPick = controller::sendNearby, onCancel = controller::cancelNearby,
                        )
                        // progress, speed and route for every device sent to, like the code share's receiver list
                        val rows by controller.nearbyReceivers.collectAsState()
                        if (rows.isNotEmpty()) {
                            SectionHeader("Transfers")
                            Receivers(rows)
                        }
                        // each device waiting on its user shows the code both screens must agree on
                        sends.forEach { (id, session) ->
                            key(id) {
                                val st by session.state.collectAsState()
                                st.receivers.firstOrNull { it.status == PeerStatus.Waiting && it.sas.isNotBlank() }?.let { r ->
                                    Spacer(Modifier.height(12.dp))
                                    SectionCard { SasCode(r.sas, r.device.ifBlank { st.nearbyTo ?: "them" }) }
                                }
                            }
                        }
                    }
                    SendMode.Code -> {
                        SectionHeader("Share with a code")
                        CodeStart(ready = picked.isNotEmpty(), onStart = controller::startSend)
                    }
                }
            }
        }
    }
}

/** The code route: works across networks, with anyone who has the code or the QR. */
@Composable
private fun CodeStart(ready: Boolean, onStart: () -> Unit) = SectionCard {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        IconBadge(R.drawable.ic_qr_code_scanner, MaterialTheme.colorScheme.tertiaryContainer, MaterialTheme.colorScheme.onTertiaryContainer, 48.dp)
        Column(Modifier.weight(1f)) {
            Text("Any device, any network", style = MaterialTheme.typography.titleMediumEmphasized)
            Text(
                "Get a code, a link and a QR. Whoever has one can receive, in a browser, the CLI or this app.",
                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    Button(onClick = onStart, enabled = ready, shapes = ButtonDefaults.shapes(), modifier = Modifier.fillMaxWidth().padding(top = 16.dp)) {
        Glyph(R.drawable.ic_link, modifier = Modifier.size(ButtonDefaults.IconSize))
        Spacer(Modifier.width(ButtonDefaults.IconSpacing))
        Text(if (ready) "Create a code" else "Choose files first")
    }
}

/** Devices on the network, like Quick Share's "Send to nearby devices": tap any number, each gets a prompt and a ring. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun NearbyCard(
    nearby: NearbyState,
    sends: Map<String, SendSession>,
    ready: Boolean,
    onPick: (String) -> Unit,
    onCancel: (String) -> Unit,
) = SectionCard {
    var confirm by remember { mutableStateOf<NearbyDevice?>(null) }
    if (nearby.devices.isEmpty()) {
        Column(Modifier.fillMaxWidth().padding(vertical = 16.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            LoadingIndicator(Modifier.size(64.dp))
            Spacer(Modifier.height(12.dp))
            Text("Looking for nearby devices…", style = MaterialTheme.typography.titleMedium)
        }
    } else {
        FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp), maxItemsInEachRow = 3) {
            nearby.devices.forEach { d ->
                key(d.id) {
                    val state = sends[d.id]?.state?.collectAsState()?.value
                    val tile = state?.let(::tileOf)
                    DeviceTile(d, tile, enabled = ready || tile?.live == true) {
                        if (tile?.live == true) confirm = d else onPick(d.id)
                    }
                }
            }
        }
    }
    Text(
        when {
            !ready -> "Choose files, then tap a device. Tap more to send to several at once."
            nearby.server.isNotEmpty() -> "The server is unreachable, so only devices on this Wi-Fi show up."
            nearby.devices.isEmpty() -> "Open WarpDrop on another device on the same Wi-Fi. Nobody there? Use a code."
            else -> "Tap a device to send. Tap it again to stop."
        },
        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 12.dp),
    )
    confirm?.let { d ->
        AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("Stop sending to ${deviceTitle(d.name).first}?") },
            text = { Text("They keep whatever already arrived in full; the rest is cancelled.") },
            confirmButton = { TextButton({ onCancel(d.id); confirm = null }) { Text("Stop") } },
            dismissButton = { TextButton({ confirm = null }) { Text("Keep sending") } },
        )
    }
}

/** What a device tile shows about the send to it. */
private data class Tile(val label: String, val live: Boolean, val error: Boolean = false, val progress: Float? = null, val done: Boolean = false)

private val reasons = mapOf("receiver declined" to "Declined", "device is busy" to "Busy", "no response" to "No response", "cancelled" to "Cancelled")

private fun tileOf(s: SendState): Tile {
    val r = s.receivers.lastOrNull()
    return when {
        r?.status == PeerStatus.Done -> Tile("Sent", live = false, done = true)
        r?.status == PeerStatus.Failed -> Tile(reasons[r.reason] ?: r.reason.ifBlank { "Failed" }, live = false, error = true)
        s.ended && s.error != null -> Tile(reasons[s.error] ?: s.error, live = false, error = true)
        s.ended -> Tile("Cancelled", live = false, error = true)
        r?.status == PeerStatus.Sending -> Tile("${(percent(r.sent, r.total) * 100).toInt()}%", live = true, progress = percent(r.sent, r.total))
        r?.status == PeerStatus.Waiting -> Tile(if (r.sas.isNotBlank()) r.sas.chunked(3).joinToString(" ") else "Waiting…", live = true)
        r != null -> Tile("Connecting…", live = true)
        else -> Tile("Waiting…", live = true)
    }
}

@Composable
private fun DeviceTile(d: NearbyDevice, tile: Tile?, enabled: Boolean, onClick: () -> Unit) {
    val (title, platform) = deviceTitle(d.name)
    val haptics = LocalHapticFeedback.current
    val c = MaterialTheme.colorScheme
    Column(
        Modifier.width(88.dp).clip(RoundedCornerShape(20.dp))
            .clickable(enabled = enabled, onClickLabel = if (tile?.live == true) "Stop sending to ${d.name}" else "Send to ${d.name}") {
                haptics.performHapticFeedback(HapticFeedbackType.ContextClick)
                onClick()
            }
            .alpha(if (enabled) 1f else 0.5f).padding(8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(Modifier.size(64.dp), contentAlignment = Alignment.Center) {
            val progress by animateFloatAsState(tile?.progress ?: 0f, label = "progress")
            val thin = Stroke(with(LocalDensity.current) { 2.dp.toPx() }, cap = StrokeCap.Round)
            when {
                tile == null -> {}
                tile.live && tile.progress != null -> CircularWavyProgressIndicator(
                    progress = { progress }, Modifier.fillMaxSize(), stroke = thin, trackStroke = thin, wavelength = 12.dp,
                )
                tile.live -> CircularWavyProgressIndicator(Modifier.fillMaxSize(), stroke = thin, trackStroke = thin, wavelength = 12.dp)
                else -> CircularProgressIndicator(
                    progress = { 1f }, Modifier.fillMaxSize(), strokeWidth = 2.dp,
                    color = if (tile.error) c.error.copy(alpha = 0.6f) else c.primary,
                )
            }
            DeviceAvatar(d.name, 50.dp)
            if (tile?.done == true) Box(
                Modifier.align(Alignment.BottomEnd).size(20.dp).background(c.primary, CircleShape),
                contentAlignment = Alignment.Center,
            ) { Glyph(R.drawable.ic_check, tint = c.onPrimary, modifier = Modifier.size(14.dp)) }
        }
        Text(title, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp))
        // how it is found is noise; what it is (Linux, Android, Firefox's OS) tells devices apart
        AnimatedContent(tile?.label ?: platform.ifBlank { "Nearby" }, transitionSpec = { fadeIn() togetherWith fadeOut() }, label = "status") { label ->
            Text(
                label,
                style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis,
                color = if (tile?.error == true) c.error else c.onSurfaceVariant,
            )
        }
    }
}

@Composable
fun ThisDevice(name: String, hint: String) = SegmentedListItem(
    shapes = ListItemDefaults.segmentedShapes(0, 1),
    colors = rowColors(),
    leadingContent = {
        IconBadge(R.drawable.ic_smartphone, MaterialTheme.colorScheme.secondaryContainer, MaterialTheme.colorScheme.onSecondaryContainer)
    },
    supportingContent = { Text(hint) },
) { Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis) }

/** One file in a group. [leading] replaces the type icon (a progress ring); [trailing] is the remove button. */
@Composable
fun FileItem(
    name: String, detail: String, icon: Int, index: Int, count: Int,
    iconTint: Color? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
) =
    SegmentedListItem(
        shapes = ListItemDefaults.segmentedShapes(index, count),
        colors = rowColors(),
        leadingContent = leading ?: { Glyph(icon, tint = iconTint ?: LocalContentColor.current) },
        supportingContent = { Text(detail) },
        trailingContent = trailing,
    ) { Text(name, maxLines = 1, overflow = TextOverflow.MiddleEllipsis) }

@Composable
private fun SharingPage(session: SendSession, platform: Platform, onClose: () -> Unit) {
    val state by session.state.collectAsState()
    val room = state.room
    val nearbyTo = state.nearbyTo
    TabScaffold(
        if (nearbyTo != null) "Sending" else "Send",
        navigationIcon = {
            IconButton(onClick = onClose) { Glyph(R.drawable.ic_close, if (state.ended) "Close" else "Stop sharing") }
        },
        floatingActionButton = {
            when {
                state.ended -> ExtendedFloatingActionButton(
                    text = { Text("Done") }, icon = { Glyph(R.drawable.ic_check) }, onClick = onClose, expanded = true,
                )
                room != null -> ExtendedFloatingActionButton(
                    text = { Text("Share link") }, icon = { Glyph(R.drawable.ic_share) }, onClick = { platform.share(room.link) }, expanded = true,
                )
            }
        },
    ) {
        state.error?.let { SectionHeader("Sharing stopped"); ErrorRow(it) }
        if (nearbyTo != null) {
            SectionHeader("Receiver")
            if (state.receivers.isEmpty() && state.error == null) SectionCard { Waiting("Waiting for $nearbyTo to respond…") }
            else Receivers(state.receivers)
            state.receivers.firstOrNull { it.status == PeerStatus.Waiting && it.sas.isNotBlank() }?.let {
                Spacer(Modifier.height(12.dp))
                SectionCard { SasCode(it.sas, it.device.ifBlank { nearbyTo }) }
            }
        } else if (room != null) {
            SectionHeader("Share code")
            CodeCard(room, platform)
            SectionHeader("Receivers")
            Receivers(state.receivers)
        } else if (state.error == null) {
            SectionHeader("Share code")
            SectionCard { Waiting(state.message.ifBlank { "Creating your code…" }) }
        }
        SectionHeader(dotted(filesLabel(state.files.size), formatBytes(state.files.sumOf { it.size })))
        val (media, docs) = state.files.partition { isMedia(it.mime) }
        if (media.isNotEmpty()) MediaCarousel(media.map { it.asMedia() })
        if (media.isNotEmpty() && docs.isNotEmpty()) Spacer(Modifier.height(ListItemDefaults.SegmentedGap * 4))
        RowGroup {
            docs.forEachIndexed { i, f -> FileItem(f.name, formatBytes(f.size), fileIcon(f.mime, f.name), i, docs.size) }
        }
    }
}

@Composable
private fun CodeCard(room: Room, platform: Platform) = SectionCard {
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        // white backing keeps the QR scannable in dark and AMOLED themes
        Box(Modifier.background(Color.White, RoundedCornerShape(20.dp)).padding(14.dp)) {
            Image(rememberQrCodePainter(room.link), contentDescription = "QR code for ${room.code}", modifier = Modifier.size(168.dp))
        }
        Spacer(Modifier.height(16.dp))
        // a zero-width space after each hyphen lets a long code wrap between words, never inside one
        Text(room.code.replace("-", "-​"), style = MaterialTheme.typography.titleLargeEmphasized, textAlign = TextAlign.Center)
        Text(
            "Scan the code, or type it in Warpdrop on any device",
            style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center, modifier = Modifier.padding(top = 4.dp),
        )
        Spacer(Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(ButtonGroupDefaults.ConnectedSpaceBetween)) {
            FilledTonalButton(onClick = { platform.copy(room.code) }, shapes = ButtonDefaults.shapes()) {
                Glyph(R.drawable.ic_content_copy, modifier = Modifier.size(ButtonDefaults.IconSize))
                Spacer(Modifier.width(ButtonDefaults.IconSpacing))
                Text("Copy code")
            }
            FilledTonalButton(onClick = { platform.copy(room.link) }, shapes = ButtonDefaults.shapes()) {
                Glyph(R.drawable.ic_link, modifier = Modifier.size(ButtonDefaults.IconSize))
                Spacer(Modifier.width(ButtonDefaults.IconSpacing))
                Text("Copy link")
            }
        }
    }
}

@Composable
private fun Receivers(receivers: List<Receiver>) {
    if (receivers.isEmpty()) {
        SectionCard { Waiting("Waiting for someone to join…") }
        return
    }
    RowGroup {
        receivers.forEachIndexed { i, r ->
            val failed = r.status == PeerStatus.Failed
            SegmentedListItem(
                shapes = ListItemDefaults.segmentedShapes(i, receivers.size),
                colors = rowColors(),
                leadingContent = { PeerAvatar(r) },
                supportingContent = {
                    Text(
                        when (r.status) {
                            PeerStatus.Connecting -> "Connecting…"
                            PeerStatus.Authenticating -> "Verifying the code…"
                            PeerStatus.Waiting -> "Looking at the files"
                            PeerStatus.Sending -> dotted("${formatBytes(r.sent)} of ${formatBytes(r.total)}", formatSpeed(r.speed), transportLabel(r.transport))
                            PeerStatus.Done -> dotted("Delivered", transportLabel(r.transport))
                            PeerStatus.Failed -> r.reason.ifBlank { "Failed" }
                        },
                        color = if (failed) MaterialTheme.colorScheme.error else Color.Unspecified,
                    )
                },
                trailingContent = when (r.status) {
                    PeerStatus.Sending -> ({ Text("${(percent(r.sent, r.total) * 100).toInt()}%", style = MaterialTheme.typography.labelLarge) })
                    PeerStatus.Done -> ({ Glyph(R.drawable.ic_check_circle, "Delivered", tint = MaterialTheme.colorScheme.primary) })
                    else -> null
                },
            ) { Text(r.device.ifBlank { "Receiver" }, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
    }
}

/** The avatar wears the transfer as a wavy ring: spinning while connecting, filling while sending. */
@Composable
private fun PeerAvatar(r: Receiver) = Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
    when (r.status) {
        PeerStatus.Sending -> CircularWavyProgressIndicator(progress = { percent(r.sent, r.total) }, Modifier.fillMaxSize())
        PeerStatus.Connecting, PeerStatus.Authenticating -> CircularWavyProgressIndicator(Modifier.fillMaxSize())
        else -> {}
    }
    DeviceAvatar(r.device, 36.dp)
}

@Composable
fun Waiting(text: String) = Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
    LoadingIndicator(Modifier.size(40.dp))
    Text(text, style = MaterialTheme.typography.bodyLarge)
}

private fun PickedFile.asMedia() = Media(this, name, mime, (this as? AndroidFile)?.uri?.toString())
