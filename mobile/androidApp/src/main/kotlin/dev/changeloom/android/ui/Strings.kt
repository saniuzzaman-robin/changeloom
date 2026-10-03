package dev.changeloom.android.ui

import android.content.Context
import androidx.annotation.StringRes

/** Text from `strings.xml`, so ViewModels can build user-facing messages without holding a Context. */
fun interface Strings {
    fun get(@StringRes id: Int, vararg args: Any): String
}

class AndroidStrings(private val context: Context) : Strings {
    override fun get(@StringRes id: Int, vararg args: Any): String = context.getString(id, *args)
}
