package dev.changeloom.android.data

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException

/** Google profile photo URLs end in a size directive like `=s96-c`; this requests a size fit for a large avatar. */
private val GOOGLE_PHOTO_SIZE = Regex("=s\\d+-c$")
private const val AVATAR_PX = 256

/**
 * Fetches and decodes the profile photo through [client], whose disk cache keeps it across launches for as long as
 * the server allows (a day for Google photos, then revalidated by ETag).
 */
class AvatarLoader(private val client: OkHttpClient) {
    suspend fun load(url: String): Bitmap = withContext(Dispatchers.IO) {
        val sized = url.replace(GOOGLE_PHOTO_SIZE, "=s$AVATAR_PX-c").toHttpUrlOrNull()
            ?: throw IOException("Avatar URL isn't a valid http(s) URL")
        client.newCall(Request.Builder().url(sized).build()).execute().use { response ->
            if (!response.isSuccessful) throw IOException("Avatar request failed: HTTP ${response.code}")
            response.body.byteStream().use { BitmapFactory.decodeStream(it) } ?: throw IOException("Avatar response isn't an image")
        }
    }
}
