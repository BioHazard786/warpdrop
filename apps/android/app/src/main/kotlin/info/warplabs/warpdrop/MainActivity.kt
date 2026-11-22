package info.warplabs.warpdrop

import android.Manifest
import android.app.DownloadManager
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import android.graphics.Color
import androidx.activity.SystemBarStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import info.warplabs.warpdrop.ui.theme.WarpTheme
import info.warplabs.warpdrop.ui.theme.isDark
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import com.google.mlkit.common.MlKitException
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import info.warplabs.warpdrop.core.PickedFile
import info.warplabs.warpdrop.ui.WarpApp
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity(), Platform {
    private val controller get() = (application as WarpApplication).controller

    private val filePicker = registerForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { addUris(it) }
    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}
    // Android 16+ asks before an app talks to other devices on the local network (mDNS and direct connections)
    private val localNetwork = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) handle(intent)

        // The foreground service notification is hidden without this on Android 13+; the transfer still runs.
        if (Build.VERSION.SDK_INT >= 33) lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                controller.busy.filter { it }.collect {
                    if (ContextCompat.checkSelfPermission(this@MainActivity, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                        notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
                }
            }
        }

        // Visible and discoverable only while the app is on screen: it saves battery and means a stranger on the
        // network can only offer files to someone who is looking at the phone.
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                if (Build.VERSION.SDK_INT >= 36 && ContextCompat.checkSelfPermission(this@MainActivity, LOCAL_NETWORK) != PackageManager.PERMISSION_GRANTED)
                    localNetwork.launch(LOCAL_NETWORK)
                controller.nearbyOn()
                try { awaitCancellation() } finally { controller.nearbyOff() }
            }
        }

        setContent {
            val settings by controller.settings.collectAsState()
            val picked by controller.picked.collectAsState()
            val send by controller.send.collectAsState()
            val receives by controller.receives.collectAsState()
            val dark = settings.theme.isDark()
            // Bar icons follow the app's theme, not the system's: a Light app on a dark system needs dark icons.
            DisposableEffect(dark) {
                enableEdgeToEdge(
                    statusBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT) { dark },
                    navigationBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT) { dark },
                )
                onDispose {}
            }
            BackHandler(enabled = picked.isNotEmpty() || send != null || receives.isNotEmpty()) { controller.back() }
            WarpTheme(settings.theme, settings.dynamicColor) {
                // Surface sets the content color; without it text defaults to black in dark mode.
                Surface(color = MaterialTheme.colorScheme.surface) { WarpApp(controller, this) }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    /** A tapped share link opens the receive flow; the share sheet fills the "ready to send" list. */
    private fun handle(intent: Intent) {
        when (intent.action) {
            Intent.ACTION_VIEW -> intent.data?.let { controller.startReceive(it.toString()) }
            Intent.ACTION_SEND -> uriExtra(intent)?.let { addUris(listOf(it)) }
            Intent.ACTION_SEND_MULTIPLE -> addUris(urisExtra(intent))
        }
    }

    @Suppress("DEPRECATION")
    private fun uriExtra(i: Intent): Uri? =
        if (Build.VERSION.SDK_INT >= 33) i.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java) else i.getParcelableExtra(Intent.EXTRA_STREAM)

    @Suppress("DEPRECATION")
    private fun urisExtra(i: Intent): List<Uri> =
        (if (Build.VERSION.SDK_INT >= 33) i.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java) else i.getParcelableArrayListExtra(Intent.EXTRA_STREAM)) ?: emptyList()

    private fun addUris(uris: List<Uri>) {
        lifecycleScope.launch { controller.addFiles(uris.mapNotNull(::describe)) }
    }

    private fun describe(uri: Uri): PickedFile? = try {
        var name: String? = null
        var size = -1L
        contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                name = c.getString(0)
                if (!c.isNull(1)) size = c.getLong(1)
            }
        }
        if (size < 0) size = contentResolver.openFileDescriptor(uri, "r")?.use { it.statSize } ?: -1L
        if (size < 0) null else AndroidFile(uri, name ?: uri.lastPathSegment ?: "file", size, contentResolver.getType(uri) ?: "application/octet-stream")
    } catch (_: Exception) { null }

    // ---- Platform ----

    override fun pickFiles() = filePicker.launch(arrayOf("*/*"))

    override fun scanCode() {
        val options = GmsBarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).enableAutoZoom().build()
        GmsBarcodeScanning.getClient(this, options).startScan()
            .addOnSuccessListener { it.rawValue?.let(controller::startReceive) }
            .addOnFailureListener {
                // backing out of the scanner arrives here as a failure, not a cancel
                if ((it as? MlKitException)?.errorCode != MlKitException.CODE_SCANNER_CANCELLED)
                    Toast.makeText(this, R.string.scan_unavailable, Toast.LENGTH_LONG).show()
            }
    }

    override fun copy(text: String) {
        getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Warpdrop", text))
        if (Build.VERSION.SDK_INT < 33) Toast.makeText(this, R.string.copied, Toast.LENGTH_SHORT).show()
    }

    override fun share(text: String) =
        startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text), null))

    override fun openDownloads() = startActivity(Intent(DownloadManager.ACTION_VIEW_DOWNLOADS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))

    private companion object {
        const val LOCAL_NETWORK = "android.permission.ACCESS_LOCAL_NETWORK"
    }
}
