package info.warplabs.warpdrop

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.os.Build
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.launch

class WarpApplication : Application() {
    lateinit var controller: AppController
        private set

    override fun onCreate() {
        super.onCreate()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        // the name the user gave the phone ("Galaxy S23 Ultra"), not the model code ("SM-S918B")
        val name = android.provider.Settings.Global.getString(contentResolver, android.provider.Settings.Global.DEVICE_NAME) ?: Build.MODEL
        controller = AppController(GoWarpCore(this), DataStoreSettings(this), "$name on Android", scope)
        getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(TransferService.CHANNEL, getString(R.string.channel_transfers), NotificationManager.IMPORTANCE_LOW)
        )
        // A transfer only starts from something the user did in the foreground, which is what lets us start the service.
        scope.launch {
            controller.busy.filter { it }.collect {
                ContextCompat.startForegroundService(this@WarpApplication, Intent(this@WarpApplication, TransferService::class.java))
            }
        }
    }
}
