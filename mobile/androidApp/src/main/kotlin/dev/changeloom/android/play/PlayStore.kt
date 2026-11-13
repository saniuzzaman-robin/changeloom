package dev.changeloom.android.play

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import dev.changeloom.android.BuildConfig
import dev.changeloom.android.R
import dev.changeloom.android.telemetry.AppLog

private const val TAG = "PlayStore"

/** The prod listing, also from staging builds, which aren't on Play. */
private val listingUrl = "https://play.google.com/store/apps/details?id=${BuildConfig.PLAY_PACKAGE}"

/** Opens the app's Play listing so the user can rate it: the Play Store app if installed, else the browser. */
fun Context.openPlayListing() {
    val market = Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=${BuildConfig.PLAY_PACKAGE}"))
        .setPackage("com.android.vending")
    try {
        startActivity(market)
    } catch (_: ActivityNotFoundException) {
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(listingUrl)))
        } catch (e: ActivityNotFoundException) {
            AppLog.w(TAG, "No app could open the Play listing", e)
            Toast.makeText(this, R.string.cannot_open_link, Toast.LENGTH_SHORT).show()
        }
    }
}

/** Shares a short pitch with the Play link; the receiving app lets the user edit it before sending. */
fun Context.shareApp() {
    val send = Intent(Intent.ACTION_SEND)
        .setType("text/plain")
        .putExtra(Intent.EXTRA_SUBJECT, getString(R.string.share_app_subject))
        .putExtra(Intent.EXTRA_TEXT, getString(R.string.share_app_text, listingUrl))
    startActivity(Intent.createChooser(send, getString(R.string.share_app_title)))
}
