package dev.changeloom.android.ui.theme

import android.content.Context
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
