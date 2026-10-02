package dev.changeloom.android.data

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

private const val CONNECT_TIMEOUT_MS = 10_000
private const val READ_TIMEOUT_MS = 10_000

/** Google profile photo URLs end in a size directive like `=s96-c`; this requests a size fit for a large avatar. */
private val GOOGLE_PHOTO_SIZE = Regex("=s\\d+-c$")
private const val AVATAR_PX = 256

/** Fetches and decodes one small image (the profile photo). There's only one, so there's no cache beyond the caller's. */
suspend fun fetchAvatar(url: String): Bitmap = withContext(Dispatchers.IO) {
    val sized = url.replace(GOOGLE_PHOTO_SIZE, "=s$AVATAR_PX-c")
    val conn = URL(sized).openConnection() as HttpURLConnection
    try {
        conn.connectTimeout = CONNECT_TIMEOUT_MS
        conn.readTimeout = READ_TIMEOUT_MS
        if (conn.responseCode != HttpURLConnection.HTTP_OK) throw IOException("Avatar request failed: HTTP ${conn.responseCode}")
        conn.inputStream.use { BitmapFactory.decodeStream(it) } ?: throw IOException("Avatar response isn't an image")
    } finally {
        conn.disconnect()
    }
}
