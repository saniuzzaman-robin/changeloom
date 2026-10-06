package dev.changeloom.android.push

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Re-aims the queued daily digest at the right local time after the time zone or the clock changes. */
class DigestClockReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        rescheduleDailyDigest(context)
    }
}
