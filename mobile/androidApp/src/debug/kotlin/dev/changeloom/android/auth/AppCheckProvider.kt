package dev.changeloom.android.auth

import com.google.firebase.appcheck.AppCheckProviderFactory
import com.google.firebase.appcheck.debug.DebugAppCheckProviderFactory

/**
 * Debug builds can't pass Play Integrity, so they use the debug provider. It logs a debug secret once
 * (`adb logcat | grep DebugAppCheckProvider`); add it in Firebase → App Check → Apps → Manage debug tokens.
 */
internal fun appCheckProviderFactory(): AppCheckProviderFactory = DebugAppCheckProviderFactory.getInstance()
