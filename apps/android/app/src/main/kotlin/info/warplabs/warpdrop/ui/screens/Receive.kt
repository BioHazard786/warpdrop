@file:OptIn(ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui.screens

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import info.warplabs.warpdrop.AppController
import info.warplabs.warpdrop.Platform
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.core.ReceiveSession
import info.warplabs.warpdrop.core.ReceiveState
import info.warplabs.warpdrop.core.ReceiveStatus
import info.warplabs.warpdrop.ui.*

@Composable
fun ReceiveTab(controller: AppController, platform: Platform) {
    val sessions by controller.receives.collectAsState()
    val nearby by controller.nearbyState.collectAsState()
    if (sessions.isNotEmpty()) RequestsPage(sessions, platform, controller)
    else TabScaffold("Receive", floatingActionButton = {
        ExtendedFloatingActionButton(
            text = { Text("Scan QR code") }, icon = { Glyph(R.drawable.ic_qr_code_scanner) }, onClick = platform::scanCode, expanded = true,
        )
    }) {
        SectionHeader("You'll appear as")
        VisibilityRow(controller.deviceName.collectAsState().value, nearby.visible, nearby.server) { controller.setVisible(!nearby.visible) }
        SectionHeader("Enter a code")
        CodeField(controller::startReceive)
    }
}

/** This device as others see it, with the switch for being listed at all (Quick Share's "visible to everyone"). */
@Composable
private fun VisibilityRow(name: String, visible: Boolean, server: String, onToggle: () -> Unit) = SegmentedListItem(
    onClick = onToggle,
    shapes = ListItemDefaults.segmentedShapes(0, 1),
    colors = rowColors(),
    leadingContent = {
        IconBadge(R.drawable.ic_smartphone, MaterialTheme.colorScheme.secondaryContainer, MaterialTheme.colorScheme.onSecondaryContainer)
    },
    supportingContent = {
        Text(
            dotted(
                if (visible) "Visible to devices on this network" else "Hidden: others can't send to you",
                if (server.isNotEmpty()) "Server unreachable" else "",
            ),
        )
    },
    trailingContent = { IconSwitch(visible) },
) { Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis) }

@Composable
private fun CodeField(onReceive: (String) -> Unit) {
    var code by remember { mutableStateOf("") }
    val submit = { if (code.isNotBlank()) onReceive(code) }
    OutlinedTextField(
        value = code,
        onValueChange = { code = it },
        placeholder = { Text("4821-amber-otter-kite-moth") },
        supportingText = { Text("The code or link the sender shared with you") },
        singleLine = true,
        shape = MaterialTheme.shapes.extraLarge,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
        keyboardActions = KeyboardActions(onGo = { submit() }),
        trailingIcon = {
            FilledIconButton(
                onClick = submit, enabled = code.isNotBlank(), shapes = IconButtonDefaults.shapes(),
                modifier = Modifier.padding(end = 6.dp),
            ) { Glyph(R.drawable.ic_download, "Receive") }
        },
        modifier = Modifier.fillMaxWidth(),
    )
}

/** Quick Share's "Sharing with you": one card per transfer, several at once when several devices send. */
@Composable
private fun RequestsPage(sessions: List<ReceiveSession>, platform: Platform, controller: AppController) {
    val states = sessions.map { it.state.collectAsState().value }
    val allEnded = states.all { it.ended }
    TabScaffold(
        "Receive",
        navigationIcon = {
            if (allEnded) IconButton(onClick = controller::clearReceived) { Glyph(R.drawable.ic_close, "Close") }
        },
        floatingActionButton = {
            if (allEnded) ExtendedFloatingActionButton(
                text = { Text("Done") }, icon = { Glyph(R.drawable.ic_check) }, onClick = controller::clearReceived, expanded = true,
            )
        },
    ) {
        SectionHeader(if (sessions.size > 1) "Sharing with you · ${sessions.size}" else "Sharing with you")
        sessions.forEachIndexed { i, session ->
            key(session) {
                if (i > 0) Spacer(Modifier.height(12.dp))
                Request(session, states[i], platform) { controller.closeReceive(session) }
            }
        }
    }
}

@Composable
private fun Request(session: ReceiveSession, state: ReceiveState, platform: Platform, onClose: () -> Unit) {
    val offer = state.offer
    RequestCard(state, session, platform, onClose)
    if (offer == null) return
    // Saved photos and videos move into the carousel as they land; everything else stays a row.
    val shown = offer.indices.partition { i -> isMedia(offer[i].mime) && state.saved[i] != null }
    if (shown.first.isNotEmpty()) {
        Spacer(Modifier.height(ListItemDefaults.SegmentedGap * 4))
        MediaCarousel(shown.first.map { i -> Media(i, offer[i].name, offer[i].mime, state.saved[i]) })
    }
    if (shown.second.isEmpty()) return
    Spacer(Modifier.height(ListItemDefaults.SegmentedGap * 4))
    RowGroup {
        shown.second.forEachIndexed { pos, i ->
            val f = offer[i]
            val done = i in state.done
            val got = state.received[i] ?: 0L
            val arriving = !state.ended && !done && got > 0
            FileItem(
                f.name,
                when {
                    done -> "${formatBytes(f.size)} · Saved"
                    arriving -> "${formatBytes(got)} of ${formatBytes(f.size)}"
                    else -> formatBytes(f.size)
                },
                if (done) R.drawable.ic_check_circle else fileIcon(f.mime, f.name),
                pos, shown.second.size,
                iconTint = if (done) MaterialTheme.colorScheme.primary else null,
                leading = if (arriving) ({ FileProgress(percent(got, f.size)) }) else null,
            )
        }
    }
}

/** Quick Share-style request: who is sending, where it stands, and the one decision to make. */
@Composable
private fun RequestCard(state: ReceiveState, session: ReceiveSession, platform: Platform, onClose: () -> Unit) = SectionCard {
    val c = MaterialTheme.colorScheme
    val offer = state.offer
    val sender = state.sender.ifBlank { "Sender" }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        if (offer == null) IconBadge(R.drawable.ic_download, c.secondaryContainer, c.onSecondaryContainer, 48.dp)
        else DeviceAvatar(sender, 48.dp)
        Column(Modifier.weight(1f)) {
            Text(
                if (offer == null) "Connecting to the sender" else sender,
                style = MaterialTheme.typography.titleMediumEmphasized, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Text(
                when {
                    state.error != null -> state.error!!
                    state.succeeded -> "Saved to Downloads"
                    offer == null -> receiveStatusText(state)
                    state.awaitingAnswer -> "Wants to share ${filesLabel(offer.size)} (${formatBytes(state.total)})"
                    state.ended -> "Transfer stopped"
                    state.status == ReceiveStatus.Reconnecting -> receiveStatusText(state)
                    else -> dotted("Receiving", transportLabel(state.transport))
                },
                style = MaterialTheme.typography.bodyMedium,
                color = if (state.error != null) c.error else c.onSurfaceVariant,
            )
        }
    }
    if (state.awaitingAnswer && state.sas.isNotBlank()) Column(Modifier.padding(top = 16.dp)) { SasCode(state.sas, sender) }
    when {
        state.ended || state.awaitingAnswer -> {}
        offer == null -> LinearWavyProgressIndicator(Modifier.fillMaxWidth().padding(top = 20.dp))
        else -> ReceivingStats(state)
    }
    Row(Modifier.fillMaxWidth().padding(top = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
        when {
            state.awaitingAnswer -> {
                OutlinedButton(onClick = session::decline, shapes = ButtonDefaults.shapes()) { Text("Decline") }
                Button(onClick = session::accept, shapes = ButtonDefaults.shapes()) { Text("Accept") }
            }
            state.succeeded -> {
                TextButton(onClick = onClose, shapes = ButtonDefaults.shapes()) { Text("Dismiss") }
                FilledTonalButton(onClick = platform::openDownloads, shapes = ButtonDefaults.shapes()) {
                    Glyph(R.drawable.ic_folder_open, modifier = Modifier.size(ButtonDefaults.IconSize))
                    Spacer(Modifier.width(ButtonDefaults.IconSpacing))
                    Text("View downloads")
                }
            }
            state.ended -> TextButton(onClick = onClose, shapes = ButtonDefaults.shapes()) { Text("Dismiss") }
            else -> OutlinedButton(onClick = onClose, shapes = ButtonDefaults.shapes()) { Text("Cancel") }
        }
    }
}

/** The web app's readout: big percentage, speed and time left, the bar, then bytes and files. */
@Composable
private fun ReceivingStats(state: ReceiveState) {
    val c = MaterialTheme.colorScheme
    val fraction = percent(state.receivedBytes, state.total)
    Row(Modifier.fillMaxWidth().padding(top = 20.dp), verticalAlignment = Alignment.Bottom) {
        Text("${(fraction * 100).toInt()}%", style = MaterialTheme.typography.displaySmallEmphasized)
        Spacer(Modifier.weight(1f))
        Column(horizontalAlignment = Alignment.End) {
            Text(formatSpeed(state.speed), style = MaterialTheme.typography.labelLarge)
            Text(
                "${formatEta(eta(state.total - state.receivedBytes, state.speed))} left",
                style = MaterialTheme.typography.bodySmall, color = c.onSurfaceVariant,
            )
        }
    }
    LinearWavyProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp))
    Row(Modifier.fillMaxWidth()) {
        Text("${formatBytes(state.receivedBytes)} of ${formatBytes(state.total)}", style = MaterialTheme.typography.bodySmall, color = c.onSurfaceVariant)
        Spacer(Modifier.weight(1f))
        Text("${state.done.size}/${state.offer?.size ?: 0} files", style = MaterialTheme.typography.bodySmall, color = c.onSurfaceVariant)
    }
}
