package dev.changeloom.android.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.CloudOff
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.ThemeMode

@Composable
private fun Gallery() {
    val c = ChangeloomTheme.colors
    Box(Modifier.background(c.bg)) {
        GridBackground(Modifier.matchParentSize())
        SpotlightGlow(Modifier.matchParentSize())
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                LoomMark(Modifier.size(56.dp))
                GradientText("Changeloom", style = MaterialTheme.typography.displaySmall)
            }
            WordRiseText("Every release that matters, woven into one feed.", style = MaterialTheme.typography.titleMedium, color = c.fgMuted)
            PrimaryButton("Continue", onClick = {}, modifier = Modifier.fillMaxWidth())
            SecondaryButton("Create account", onClick = {}, modifier = Modifier.fillMaxWidth())
            GlassCard(onClick = {}) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    KindPill("security")
                    SeverityDot("critical")
                    Eyebrow("2h ago")
                    Box(Modifier.weight(1f))
                    ImportanceMeter(4)
                    SaveToggle(saved = true, onToggle = {})
                }
                Text("Go 1.26.1 fixes a critical net/http flaw", style = MaterialTheme.typography.titleMedium, color = c.fg)
                Text("Upgrade now: request smuggling in the HTTP/2 server.", style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                KindPill("release"); KindPill("breaking"); KindPill("deprecation"); KindPill("announcement"); KindPill("article")
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                TopicChip("Go", selected = true, onClick = {})
                TopicChip("Rust", onClick = {})
                IconTile(Icons.Rounded.Code)
                GradientAvatar("Saniuzzaman Robin", size = 48.dp)
            }
            StatusBanner("You're offline. Showing saved stories.", Icons.Rounded.CloudOff, tone = BannerTone.Warning, actionLabel = "Retry")
            SkeletonBlock(Modifier.fillMaxWidth(0.7f))
        }
    }
}

@Preview(name = "Dark", heightDp = 900)
@Composable
private fun GalleryDark() = ChangeloomTheme(ThemeMode.Dark) { Gallery() }

@Preview(name = "Light", heightDp = 900)
@Composable
private fun GalleryLight() = ChangeloomTheme(ThemeMode.Light) { Gallery() }
