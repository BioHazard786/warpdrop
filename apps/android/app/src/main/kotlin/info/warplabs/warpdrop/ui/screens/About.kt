@file:OptIn(ExperimentalMaterial3ExpressiveApi::class)

package info.warplabs.warpdrop.ui.screens

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.mikepenz.aboutlibraries.ui.compose.android.produceLibraries
import com.mikepenz.aboutlibraries.ui.compose.m3.LibrariesContainer
import info.warplabs.warpdrop.BuildConfig
import info.warplabs.warpdrop.R
import info.warplabs.warpdrop.ui.*
import info.warplabs.warpdrop.ui.theme.RoundedFontFamily

private const val GITHUB = "https://github.com/BioHazard786"

private class FamilyApp(val name: String, val blurb: String, val live: String?, val repo: String, val here: Boolean = false)

// Same apps and links as web/src/routes/about.tsx (and Warpstash's about page); blurbs are shorter for a phone.
private val FAMILY = listOf(
    FamilyApp("Warpstash", "Temporary file hosting", "https://stash.warplabs.info", "$GITHUB/warpstash"),
    FamilyApp("Warpdrop", "Device-to-device file sharing", "https://drop.warplabs.info", "$GITHUB/warpdrop", here = true),
    FamilyApp("Warpvault", "Zero-knowledge secret sharing", "https://vault.warplabs.info", "$GITHUB/warpvault"),
    FamilyApp("Warpkey", "Self-hosted credential vault", null, "$GITHUB/warpkey"),
    FamilyApp("Warptv", "Free live TV channels", "https://tv.warplabs.info", "$GITHUB/warptv"),
)

/** The web app's /about, as Android does it: a page under Settings with the app, its family, the developer and licenses. */
@Composable
fun AboutPage(onBack: () -> Unit, onLicenses: () -> Unit) {
    val uri = LocalUriHandler.current
    TabScaffold("About", navigationIcon = { BackButton(onBack) }) {
        AppHeader()

        SectionHeader("The Warp family")
        RowGroup {
            FAMILY.forEachIndexed { i, app ->
                SegmentedListItem(
                    onClick = { uri.openUri(app.live ?: app.repo) },
                    shapes = ListItemDefaults.segmentedShapes(i, FAMILY.size),
                    colors = rowColors(),
                    leadingContent = { DeviceAvatar(app.name) },
                    overlineContent = if (app.here) ({ Text("You are here") }) else null,
                    supportingContent = { Text(app.blurb) },
                    // the row opens the live app (or the source when there is none); the source always has its own button
                    trailingContent = {
                        IconButton(onClick = { uri.openUri(app.repo) }) { Glyph(R.drawable.ic_code, "${app.name} source code") }
                    },
                ) { Text(app.name) }
            }
        }

        SectionHeader("The developer")
        SegmentedListItem(
            onClick = { uri.openUri(GITHUB) },
            shapes = ListItemDefaults.segmentedShapes(0, 1),
            colors = rowColors(),
            leadingContent = { DeviceAvatar("Mohd Zaid") },
            supportingContent = { Text("@BioHazard786 on GitHub") },
            trailingContent = { Glyph(R.drawable.ic_open_in_new, null) },
        ) { Text("Mohd Zaid") }

        SectionHeader("Legal")
        RowGroup {
            SegmentedListItem(
                shapes = ListItemDefaults.segmentedShapes(0, 2),
                colors = rowColors(),
                leadingContent = { Glyph(R.drawable.ic_lock) },
                supportingContent = { Text("End-to-end encrypted. Nothing is stored on the server.") },
            ) { Text("Privacy") }
            SegmentedListItem(
                onClick = onLicenses,
                shapes = ListItemDefaults.segmentedShapes(1, 2),
                colors = rowColors(),
                leadingContent = { Glyph(R.drawable.ic_license) },
                supportingContent = { Text("Libraries Warpdrop is built on") },
            ) { Text("Open-source licenses") }
        }
    }
}

/** The launcher icon, name and version, centred like the top of Android's own "About" screens. */
@Composable
private fun AppHeader() = Column(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
    // painterResource can't draw an adaptive icon, so rebuild it from its layers
    Image(
        painterResource(R.drawable.ic_launcher_foreground), null,
        Modifier.size(88.dp).clip(RoundedCornerShape(percent = 30)).background(Color.Black),
    )
    Text("Warpdrop", style = MaterialTheme.typography.headlineSmall.copy(fontFamily = RoundedFontFamily), modifier = Modifier.padding(top = 12.dp))
    Text(
        "Version ${BuildConfig.VERSION_NAME}",
        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center,
    )
}

/** Every library in the APK and its license, collected at build time by the AboutLibraries Gradle plugin. */
@Composable
fun LicensesPage(onBack: () -> Unit) {
    val libraries by produceLibraries(R.raw.aboutlibraries)
    PageScaffold("Licenses", navigationIcon = { BackButton(onBack) }) { pad ->
        LibrariesContainer(libraries, Modifier.fillMaxSize(), contentPadding = pad)
    }
}
