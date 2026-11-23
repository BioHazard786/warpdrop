package info.warplabs.warpdrop

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import info.warplabs.warpdrop.core.Settings
import info.warplabs.warpdrop.core.SettingsStore
import info.warplabs.warpdrop.core.ThemeMode
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore("settings")

class DataStoreSettings(private val context: Context) : SettingsStore {
    private val server = stringPreferencesKey("server")
    private val relay = booleanPreferencesKey("force_relay")
    private val theme = stringPreferencesKey("theme")
    private val dynamic = booleanPreferencesKey("dynamic_color")
    private val visible = booleanPreferencesKey("nearby_visible")
    private val name = stringPreferencesKey("device_name")

    override val settings: Flow<Settings> = context.dataStore.data.map {
        Settings(
            server = it[server] ?: "",
            forceRelay = it[relay] ?: false,
            theme = it[theme]?.let { t -> ThemeMode.entries.firstOrNull { m -> m.name == t } } ?: ThemeMode.System,
            dynamicColor = it[dynamic] ?: true,
            nearbyVisible = it[visible] ?: true,
            deviceName = it[name] ?: "",
        )
    }

    override suspend fun save(settings: Settings) {
        context.dataStore.edit {
            it[server] = settings.server
            it[relay] = settings.forceRelay
            it[theme] = settings.theme.name
            it[dynamic] = settings.dynamicColor
            it[visible] = settings.nearbyVisible
            it[name] = settings.deviceName
        }
    }
}
