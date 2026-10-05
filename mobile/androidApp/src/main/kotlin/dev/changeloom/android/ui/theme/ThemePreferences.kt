package dev.changeloom.android.ui.theme

import android.app.UiModeManager
import android.content.Context
import android.os.Build
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** The user's light/dark choice, kept on device. Defaults to dark like the website. */
class ThemePreferences(context: Context) {
    private val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val _mode = MutableStateFlow(read())
    val mode: StateFlow<ThemeMode> = _mode.asStateFlow()

    fun setMode(mode: ThemeMode) {
        prefs.edit().putString(KEY_MODE, mode.name).apply()
        _mode.value = mode
    }

    private fun read(): ThemeMode =
        prefs.getString(KEY_MODE, null)?.let { saved -> ThemeMode.entries.firstOrNull { it.name == saved } } ?: ThemeMode.Dark

    private companion object {
        const val PREFS = "changeloom_ui"
        const val KEY_MODE = "theme_mode"
    }
}

/**
 * Android 12+: hands the choice to the system, which keeps it for this app. The launch splash is drawn before the app
 * runs, so only this makes it (and the night resources) follow the choice rather than the system's; earlier versions
 * always show the system's.
 */
fun Context.applyNightMode(mode: ThemeMode) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
    val night = when (mode) {
        ThemeMode.System -> UiModeManager.MODE_NIGHT_AUTO
        ThemeMode.Light -> UiModeManager.MODE_NIGHT_NO
        ThemeMode.Dark -> UiModeManager.MODE_NIGHT_YES
    }
    getSystemService(UiModeManager::class.java).setApplicationNightMode(night)
}
