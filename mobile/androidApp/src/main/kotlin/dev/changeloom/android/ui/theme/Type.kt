package dev.changeloom.android.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.googlefonts.Font
import androidx.compose.ui.text.googlefonts.GoogleFont
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import dev.changeloom.android.R

// Downloadable fonts via Play Services; without it Compose falls back to the system font.
private val provider = GoogleFont.Provider(
    providerAuthority = "com.google.android.gms.fonts",
    providerPackage = "com.google.android.gms",
    certificates = R.array.com_google_android_gms_fonts_certs,
)

private fun googleFamily(name: String, vararg weights: FontWeight) =
    FontFamily(weights.map { Font(googleFont = GoogleFont(name), fontProvider = provider, weight = it) })

val HeadingFamily = googleFamily("Plus Jakarta Sans", FontWeight.Medium, FontWeight.SemiBold, FontWeight.Bold, FontWeight.ExtraBold)
val BodyFamily = googleFamily("Inter", FontWeight.Normal, FontWeight.Medium, FontWeight.SemiBold)
val MonoFamily = googleFamily("JetBrains Mono", FontWeight.Normal, FontWeight.Medium, FontWeight.SemiBold)

private val tight = (-0.025).em

private fun heading(size: Int, line: Int, weight: FontWeight = FontWeight.SemiBold) = TextStyle(
    fontFamily = HeadingFamily,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = tight,
)

private fun body(size: Int, line: Int, weight: FontWeight = FontWeight.Normal) = TextStyle(
    fontFamily = BodyFamily,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
)

val ChangeloomTypography = Typography(
    displayLarge = heading(48, 52, FontWeight.ExtraBold).copy(letterSpacing = (-0.04).em),
    displayMedium = heading(40, 44, FontWeight.Bold).copy(letterSpacing = (-0.035).em),
    displaySmall = heading(34, 40, FontWeight.Bold),
    headlineLarge = heading(30, 36, FontWeight.Bold),
    headlineMedium = heading(26, 32),
    headlineSmall = heading(22, 28),
    titleLarge = heading(20, 26),
    titleMedium = heading(16, 22),
    titleSmall = heading(14, 20),
    bodyLarge = body(16, 26),
    bodyMedium = body(14, 22),
    bodySmall = body(12, 18),
    labelLarge = body(14, 20, FontWeight.Medium),
    labelMedium = body(12, 16, FontWeight.Medium),
    labelSmall = body(11, 16, FontWeight.Medium),
)

/** Type styles M3 has no slot for. */
object ChangeloomTextStyles {
    /** Small uppercase mono meta label, e.g. `RELEASE · 2H AGO`. Callers uppercase the text. */
    val eyebrow = TextStyle(
        fontFamily = MonoFamily,
        fontWeight = FontWeight.Medium,
        fontSize = 11.sp,
        lineHeight = 16.sp,
        letterSpacing = 0.08.em,
    )
    val code = TextStyle(
        fontFamily = MonoFamily,
        fontWeight = FontWeight.Normal,
        fontSize = 13.sp,
        lineHeight = 20.sp,
    )
}
