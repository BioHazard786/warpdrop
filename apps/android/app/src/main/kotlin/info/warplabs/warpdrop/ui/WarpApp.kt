@file:OptIn(ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui

import androidx.annotation.DrawableRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import info.warplabs.warpdrop.AppController
import info.warplabs.warpdrop.Platform
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.ui.screens.ReceiveTab
import info.warplabs.warpdrop.ui.screens.SendTab
import info.warplabs.warpdrop.ui.screens.SettingsTab
import kotlinx.coroutines.flow.*

private enum class Tab(val label: String, @DrawableRes val icon: Int, @DrawableRes val selectedIcon: Int) {
    Send("Send", R.drawable.ic_upload, R.drawable.ic_upload_filled),
    Receive("Receive", R.drawable.ic_download, R.drawable.ic_download_filled),
    Settings("Settings", R.drawable.ic_settings, R.drawable.ic_settings_filled),
}

/** Root of the UI: three tabs. Transfers live in [AppController], so switching tabs never interrupts one. */
@Composable
fun WarpApp(controller: AppController, platform: Platform) {
    val send by controller.send.collectAsState()
    val sendingNearby by controller.sendingNearby.collectAsState()
    val receives by controller.receives.collectAsState()
    var tab by rememberSaveable { mutableStateOf(if (controller.receives.value.isNotEmpty()) Tab.Receive else Tab.Send) }

    // A new receive (a tapped link, a scanned code, a device sending to us) jumps to Receive; files handed over by
    // the share sheet jump to Send.
    LaunchedEffect(Unit) {
        merge(
            controller.receives.map { it.size }.runningFold(Int.MAX_VALUE to Int.MAX_VALUE) { (_, prev), n -> prev to n }
                .filter { (before, now) -> now > before }.map { Tab.Receive },
            controller.picked.drop(1).filter { it.isNotEmpty() }.map { Tab.Send },
        ).collect { tab = it }
    }

    Scaffold(
        contentWindowInsets = WindowInsets(0), // each tab's app bar takes the status bar inset itself
        bottomBar = {
            ShortNavigationBar {
                Tab.entries.forEach { t ->
                    val active = (t == Tab.Send && (send != null || sendingNearby)) || (t == Tab.Receive && receives.isNotEmpty())
                    ShortNavigationBarItem(
                        selected = tab == t,
                        onClick = { tab = t },
                        icon = {
                            BadgedBox(badge = { if (active && tab != t) Badge() }) {
                                Glyph(if (tab == t) t.selectedIcon else t.icon)
                            }
                        },
                        label = { Text(t.label) },
                    )
                }
            }
        },
    ) { pad ->
        AnimatedContent(
            targetState = tab,
            modifier = Modifier.padding(bottom = pad.calculateBottomPadding()).consumeWindowInsets(pad),
            transitionSpec = { fadeIn(tween(200)) togetherWith fadeOut(tween(120)) },
            label = "tab",
        ) { t ->
            when (t) {
                Tab.Send -> SendTab(controller, platform)
                Tab.Receive -> ReceiveTab(controller, platform)
                Tab.Settings -> SettingsTab(controller)
            }
        }
    }
}
