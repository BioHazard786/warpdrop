@file:OptIn(ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui.screens

import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.annotation.DrawableRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import info.warplabs.warpdrop.AppController
import info.warplabs.warpdrop.BuildConfig
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.core.Settings
import info.warplabs.warpdrop.core.ThemeMode
import info.warplabs.warpdrop.ui.*

@Composable
fun SettingsTab(controller: AppController) {
    val settings by controller.settings.collectAsState()
    var dialog by remember { mutableStateOf<String?>(null) }
    val save = controller::saveSettings
    // About and Licenses are pages pushed on top of Settings; back pops them one at a time.
    var page by rememberSaveable { mutableStateOf(Page.Settings) }
    BackHandler(page != Page.Settings) { page = if (page == Page.Licenses) Page.About else Page.Settings }

    AnimatedContent(page, transitionSpec = { fadeIn(tween(200)) togetherWith fadeOut(tween(120)) }, label = "page") { p ->
        when (p) {
            Page.About -> AboutPage(onBack = { page = Page.Settings }, onLicenses = { page = Page.Licenses })
            Page.Licenses -> LicensesPage(onBack = { page = Page.About })
            Page.Settings -> SettingsPage(controller, settings, save, openDialog = { dialog = it }, onAbout = { page = Page.About })
        }
    }

    when (dialog) {
        "server" -> ServerDialog(settings.server, onDismiss = { dialog = null }) { save(settings.copy(server = it)); dialog = null }
        "theme" -> ChoiceDialog("Theme", ThemeMode.entries, settings.theme, { it.label }, { dialog = null }) {
            save(settings.copy(theme = it)); dialog = null
        }
    }
}

private enum class Page { Settings, About, Licenses }

@Composable
private fun SettingsPage(controller: AppController, settings: Settings, save: (Settings) -> Unit, openDialog: (String) -> Unit, onAbout: () -> Unit) {
    TabScaffold("Settings") {
        Group("Connection", listOf(
            Item(R.drawable.ic_dns, "Server", settings.server.ifBlank { "drop.warplabs.info (default)" }) { openDialog("server") },
            Item(R.drawable.ic_sync_alt, "Always use relay", "Skip the direct connection. Slower, but works on strict networks.",
                trailing = { IconSwitch(settings.forceRelay) }) { save(settings.copy(forceRelay = !settings.forceRelay)) },
        ))
        Group("Appearance", listOfNotNull(
            Item(R.drawable.ic_brightness_6, "Theme", settings.theme.label) { openDialog("theme") },
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) Item(
                R.drawable.ic_palette, "Dynamic color", "Use colors from your wallpaper",
                trailing = { IconSwitch(settings.dynamicColor) },
            ) { save(settings.copy(dynamicColor = !settings.dynamicColor)) } else null,
        ))
        Group("About", listOf(
            Item(R.drawable.ic_smartphone, "Device name", controller.deviceName, onClick = null),
            Item(R.drawable.ic_info, "About Warpdrop", "Version ${BuildConfig.VERSION_NAME}", onClick = onAbout),
        ))
    }
}

private class Item(
    @DrawableRes val icon: Int,
    val title: String,
    val subtitle: String?,
    val trailing: (@Composable () -> Unit)? = null,
    val onClick: (() -> Unit)?,
)

@Composable
private fun Group(title: String, rows: List<Item>) {
    SectionHeader(title)
    RowGroup {
        rows.forEachIndexed { i, r ->
            val shapes = ListItemDefaults.segmentedShapes(i, rows.size)
            val leading: @Composable () -> Unit = { Glyph(r.icon) }
            val supporting: (@Composable () -> Unit)? = r.subtitle?.let { { Text(it) } }
            if (r.onClick != null) SegmentedListItem(
                onClick = r.onClick, shapes = shapes, colors = rowColors(),
                leadingContent = leading, supportingContent = supporting, trailingContent = r.trailing,
            ) { Text(r.title) }
            else SegmentedListItem(
                shapes = shapes, colors = rowColors(),
                leadingContent = leading, supportingContent = supporting, trailingContent = r.trailing,
            ) { Text(r.title) }
        }
    }
}

@Composable
private fun <T> ChoiceDialog(title: String, options: List<T>, selected: T, label: (T) -> String, onDismiss: () -> Unit, onPick: (T) -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
        title = { Text(title) },
        text = {
            Column(Modifier.selectableGroup()) {
                options.forEach { o ->
                    Row(
                        Modifier.fillMaxWidth().height(56.dp).selectable(o == selected, onClick = { onPick(o) }, role = Role.RadioButton),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(o == selected, null)
                        Spacer(Modifier.width(16.dp))
                        Text(label(o), style = MaterialTheme.typography.bodyLarge)
                    }
                }
            }
        },
    )
}

@Composable
private fun ServerDialog(current: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var server by remember { mutableStateOf(current) }
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Glyph(R.drawable.ic_dns) },
        title = { Text("Server") },
        text = {
            OutlinedTextField(
                value = server,
                onValueChange = { server = it },
                placeholder = { Text("drop.warplabs.info") },
                supportingText = { Text("Leave empty for the hosted server, or enter your own.") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
        },
        confirmButton = { TextButton(onClick = { onSave(server.trim()) }) { Text("Save") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
