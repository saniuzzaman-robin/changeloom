package dev.changeloom.android.ads

import android.content.Context
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.res.ResourcesCompat
import com.google.android.gms.ads.nativead.MediaView
import com.google.android.gms.ads.nativead.NativeAd
import com.google.android.gms.ads.nativead.NativeAdView
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.theme.ChangeloomTheme

private const val ICON_DP = 36f
private const val GAP_DP = 10f
private const val BADGE_PAD_H_DP = 6f
private const val BADGE_PAD_V_DP = 2f
private const val BADGE_RADIUS_DP = 6f
private const val CTA_PAD_H_DP = 16f
private const val CTA_PAD_V_DP = 8f
private const val CTA_RADIUS_DP = 999f
private const val BADGE_SP = 11f
private const val HEADLINE_SP = 16f
private const val BODY_SP = 14f
private const val CTA_SP = 14f
private const val BODY_MAX_LINES = 2

/** The theme colors the ad's views use; they're plain Views, so they get ARGB ints. */
private data class AdColors(val fg: Int, val muted: Int, val badgeBg: Int, val cta: Int, val onCta: Int)

/**
 * A native ad styled like a story card. AdMob requires the visible "Sponsored" badge, the AdChoices icon (the SDK
 * adds it, top right) and, when the ad has media, a [MediaView]; never show one outside the feed.
 */
@Composable
fun NativeAdCard(ad: NativeAd, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val colors = AdColors(c.fg.toArgb(), c.fgMuted.toArgb(), c.surface2.toArgb(), c.primary.toArgb(), c.primaryFg.toArgb())
    val badge = stringResource(R.string.ad_badge)
    GlassCard(modifier.testTag("ad_card")) {
        AndroidView(
            factory = { context -> AdViews(context).root },
            modifier = Modifier.fillMaxWidth(),
            onReset = {}, // the view is reused in the feed; update binds the next ad
            onRelease = NativeAdView::destroy,
            update = { (it.tag as AdViews).bind(ad, colors, badge) },
        )
    }
}

private class AdViews(context: Context) {
    val root = NativeAdView(context)
    private val font = ResourcesCompat.getFont(context, R.font.inter)
    private val badge = text(context, BADGE_SP).apply {
        setPadding(dp(BADGE_PAD_H_DP), dp(BADGE_PAD_V_DP), dp(BADGE_PAD_H_DP), dp(BADGE_PAD_V_DP))
    }
    private val icon = ImageView(context).apply { scaleType = ImageView.ScaleType.CENTER_CROP }
    private val headline = text(context, HEADLINE_SP, Typeface.BOLD).apply { maxLines = 2 }
    private val advertiser = text(context, BADGE_SP).apply { maxLines = 1 }
    private val body = text(context, BODY_SP).apply { maxLines = BODY_MAX_LINES }
    private val media = MediaView(context).apply { setImageScaleType(ImageView.ScaleType.CENTER_CROP) }
    private val cta = text(context, CTA_SP, Typeface.BOLD).apply {
        gravity = Gravity.CENTER
        setPadding(dp(CTA_PAD_H_DP), dp(CTA_PAD_V_DP), dp(CTA_PAD_H_DP), dp(CTA_PAD_V_DP))
    }

    init {
        val titles = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            addView(headline)
            addView(advertiser)
        }
        val header = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            addView(icon, LinearLayout.LayoutParams(dp(ICON_DP), dp(ICON_DP)).apply { marginEnd = dp(GAP_DP) })
            addView(titles, LinearLayout.LayoutParams(0, WRAP_CONTENT, 1f))
        }
        val column = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            addView(badge, LinearLayout.LayoutParams(WRAP_CONTENT, WRAP_CONTENT).apply { bottomMargin = dp(GAP_DP) })
            addView(header)
            addView(body, gapAbove())
            addView(media, gapAbove(MATCH_PARENT))
            addView(cta, gapAbove().apply { gravity = Gravity.END })
        }
        root.addView(column, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT))
        root.headlineView = headline
        root.advertiserView = advertiser
        root.bodyView = body
        root.iconView = icon
        root.mediaView = media
        root.callToActionView = cta
        root.tag = this
    }

    fun bind(ad: NativeAd, colors: AdColors, badgeText: String) {
        badge.text = badgeText
        badge.setTextColor(colors.muted)
        badge.background = rounded(colors.badgeBg, BADGE_RADIUS_DP)
        headline.text = ad.headline
        headline.setTextColor(colors.fg)
        advertiser.show(ad.advertiser)
        advertiser.setTextColor(colors.muted)
        body.show(ad.body)
        body.setTextColor(colors.muted)
        icon.visibility = if (ad.icon?.drawable == null) View.GONE else View.VISIBLE
        icon.setImageDrawable(ad.icon?.drawable)
        media.visibility = if (ad.mediaContent == null) View.GONE else View.VISIBLE
        cta.show(ad.callToAction)
        cta.setTextColor(colors.onCta)
        cta.background = rounded(colors.cta, CTA_RADIUS_DP)
        // Last, once every asset view is set: this registers the views for impressions and clicks.
        root.setNativeAd(ad)
    }

    private fun TextView.show(value: String?) {
        text = value
        visibility = if (value.isNullOrBlank()) View.GONE else View.VISIBLE
    }

    private fun text(context: Context, sp: Float, style: Int = Typeface.NORMAL) = TextView(context).apply {
        setTextSize(TypedValue.COMPLEX_UNIT_SP, sp)
        setTypeface(font, style)
        ellipsize = android.text.TextUtils.TruncateAt.END
    }

    private fun rounded(color: Int, radiusDp: Float) = GradientDrawable().apply {
        setColor(color)
        cornerRadius = dp(radiusDp).toFloat()
    }

    private fun gapAbove(width: Int = WRAP_CONTENT) =
        LinearLayout.LayoutParams(width, WRAP_CONTENT).apply { topMargin = dp(GAP_DP) }

    private fun dp(value: Float): Int =
        TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, value, root.resources.displayMetrics).toInt()
}
