package dev.changeloom.android.ui

import android.content.Context
import android.content.Intent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.Crossfade
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.automirrored.rounded.OpenInNew
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material.icons.rounded.DoneAll
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.MarkEmailUnread
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Share
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.EmptyState
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.ImportanceMeter
import dev.changeloom.android.ui.components.KindPill
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.SeverityDot
import dev.changeloom.android.ui.components.SkeletonBlock
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTextStyles
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.android.ui.theme.kindColor
import dev.changeloom.android.ui.theme.severityColor
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StorySource
import dev.changeloom.shared.data.StorySummary
import org.koin.androidx.compose.koinViewModel
import org.koin.core.parameter.parametersOf
import java.net.URI
import java.net.URISyntaxException

@Composable
internal fun StoryDetailScreen(id: Long, onBack: () -> Unit) {
    val vm: StoryDetailViewModel = koinViewModel(key = "story-$id") { parametersOf(id) }
    val timeline: TimelineViewModel = koinViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    val timelineState by timeline.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    // The feed's copy shows the hero (and lands the shared title) before the full story loads. Read state only
    // syncs for stories in the timeline, so the Read action is hidden for the rest.
    val cached = timelineState.items.firstOrNull { it.id == id }
    StoryDetailContent(
        state = state,
        placeholder = cached,
        isRead = cached?.isRead,
        onBack = onBack,
        onRetry = vm::load,
        onToggleSave = { state.story?.let { vm.setBookmarked(!it.isBookmarked) } },
        onToggleRead = { cached?.let { vm.setRead(!it.isRead) } },
        onShare = { context.shareStory(it) },
    )
}

@Composable
internal fun StoryDetailContent(
    state: StoryDetailState,
    placeholder: StorySummary?,
    isRead: Boolean?,
    onBack: () -> Unit,
    onRetry: () -> Unit,
    onToggleSave: () -> Unit,
    onToggleRead: () -> Unit,
    onShare: (Story) -> Unit,
) {
    val c = ChangeloomTheme.colors
    val story = state.story
    val hero = story?.asSummary() ?: placeholder
    val scroll = rememberScrollState()
    val uriHandler = LocalUriHandler.current
    val navBottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()

    Box(Modifier.fillMaxSize().background(c.bg)) {
        if (hero == null && !state.loading) {
            EmptyState(
                title = "Couldn't load this story",
                message = state.error ?: "Check your connection and try again.",
                modifier = Modifier.align(Alignment.Center),
                action = { PrimaryButton("Retry", onRetry, icon = Icons.Rounded.Refresh) },
            )
        } else {
            Column(Modifier.fillMaxSize().verticalScroll(scroll)) {
                if (hero != null) DetailHero(hero) else HeroSkeleton()
                Column(Modifier.padding(horizontal = 20.dp)) {
                    when {
                        story != null -> {
                            MarkdownText(story.bodyMd, Modifier.enter(BODY_ENTER_DELAY))
                            if (story.sources.isNotEmpty()) {
                                SourceList(story.sources, onOpen = uriHandler::openUri, Modifier.padding(top = 32.dp).enter(SOURCES_ENTER_DELAY))
                            }
                        }
                        state.loading -> BodySkeleton()
                        else -> StatusBanner(
                            state.error ?: "Could not load story",
                            Icons.Rounded.ErrorOutline,
                            tone = BannerTone.Error,
                            actionLabel = "Retry",
                            onAction = onRetry,
                        )
                    }
                }
                Spacer(Modifier.height(ACTION_BAR_SPACE + navBottom))
            }
        }
        DetailTopBar(hero?.title, scroll, onBack, Modifier.align(Alignment.TopCenter))
        AnimatedVisibility(
            story != null,
            Modifier.align(Alignment.BottomCenter),
            enter = fadeIn(expoTween(Durations.SLOW)) + slideInVertically(expoTween(Durations.SLOW)) { it },
            exit = fadeOut(expoTween(Durations.FAST)),
        ) {
            if (story != null) {
                DetailActionBar(
                    saved = story.isBookmarked,
                    read = isRead,
                    onToggleSave = onToggleSave,
                    onToggleRead = onToggleRead,
                    onShare = { onShare(story) },
                )
            }
        }
    }
}

/** Kind-tinted spotlight over the grid, then meta, the shared title, the summary as a lede, importance and topics. */
@Composable
private fun DetailHero(story: StorySummary) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    val tint = kindColor(story.kind)
    Box(Modifier.fillMaxWidth()) {
        SpotlightGlow(Modifier.matchParentSize(), color = tint.copy(alpha = if (c.isDark) 0.24f else 0.14f), center = Offset(0.15f, 0f))
        GridBackground(Modifier.matchParentSize(), fadeCenter = Offset(0.3f, 0f), fadeRadius = 0.85f)
        Column(Modifier.padding(heroPadding())) {
            Row(Modifier.enter(0), verticalAlignment = Alignment.CenterVertically) {
                KindPill(story.kind)
                if (story.severity != null) {
                    SeverityDot(story.severity, Modifier.padding(start = 4.dp))
                    Eyebrow(story.severity.orEmpty(), color = severityColor(story.severity))
                }
                Spacer(Modifier.weight(1f))
                Eyebrow(relativeTime(story.publishedAt))
            }
            Spacer(Modifier.height(18.dp))
            Text(story.title, Modifier.sharedStoryTitle(story.id), style = MaterialTheme.typography.headlineMedium, color = c.fg)
            Spacer(Modifier.height(12.dp))
            Text(story.summary, Modifier.enter(LEDE_ENTER_DELAY), style = MaterialTheme.typography.bodyLarge, color = c.fgMuted)
            Spacer(Modifier.height(20.dp))
            Row(Modifier.enter(META_ENTER_DELAY), verticalAlignment = Alignment.CenterVertically) {
                ImportanceMeter(story.importance)
                Spacer(Modifier.width(8.dp))
                Eyebrow("Importance ${story.importance}/5")
            }
            if (story.topics.isNotEmpty()) {
                Spacer(Modifier.height(14.dp))
                FlowRow(
                    Modifier.enter(META_ENTER_DELAY),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) { story.topics.forEach { TopicChip(topicName(it)) } }
            }
        }
        // Hairline that fades out at both ends, separating the hero from the body.
        Box(
            Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .height(1.dp)
                .background(Brush.horizontalGradient(listOf(Color.Transparent, c.lineStrong, Color.Transparent))),
        )
    }
}

/** Room for the status bar and the floating top bar above the hero. */
@Composable
private fun heroPadding(): PaddingValues {
    val status = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
    return PaddingValues(start = 20.dp, end = 20.dp, top = status + TOP_BAR_HEIGHT + 8.dp, bottom = 28.dp)
}

@Composable
private fun HeroSkeleton() {
    Column(Modifier.padding(heroPadding())) {
        SkeletonBlock(Modifier.width(96.dp).clip(Radius.pill), height = 22.dp)
        Spacer(Modifier.height(20.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.95f), height = 26.dp)
        Spacer(Modifier.height(8.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.7f), height = 26.dp)
        Spacer(Modifier.height(16.dp))
        SkeletonBlock(Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.85f))
    }
}

@Composable
private fun BodySkeleton() {
    Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SkeletonBlock(Modifier.fillMaxWidth(0.5f), height = 20.dp)
        Spacer(Modifier.height(4.dp))
        repeat(BODY_SKELETON_LINES) { i -> SkeletonBlock(Modifier.fillMaxWidth(if (i % 3 == 2) 0.7f else 1f)) }
    }
}

/**
 * Back button and, once the hero scrolls away, a glass bar with the title. The reading progress bar runs
 * along its bottom edge and is drawn straight from [scroll], so scrolling doesn't recompose.
 */
@Composable
private fun DetailTopBar(title: String?, scroll: ScrollState, onBack: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val brand = ChangeloomTheme.gradients.brandHorizontal
    val collapseAt = with(LocalDensity.current) { TITLE_COLLAPSE_OFFSET.toPx() }
    val scrolled by remember { derivedStateOf { scroll.value > 0 } }
    val collapsed by remember { derivedStateOf { scroll.value > collapseAt } }
    val glass by animateFloatAsState(if (scrolled) 1f else 0f, expoTween(), label = "topBarGlass")
    Column(modifier.fillMaxWidth().drawBehind { drawRect(c.navGlass, alpha = glass) }) {
        Row(
            Modifier
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Top))
                .height(TOP_BAR_HEIGHT)
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(c.navGlass)
                    .border(1.dp, c.line, CircleShape)
                    .clickable(role = Role.Button, onClickLabel = "Back", onClick = onBack),
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back", tint = c.fg, modifier = Modifier.size(20.dp))
            }
            AnimatedVisibility(
                collapsed && title != null,
                Modifier.weight(1f),
                enter = fadeIn(expoTween()) + slideInVertically(expoTween()) { it / 2 },
                exit = fadeOut(expoTween(Durations.FAST)),
            ) {
                Text(
                    title.orEmpty(),
                    Modifier.padding(start = 14.dp, end = 8.dp),
                    style = MaterialTheme.typography.titleSmall,
                    color = c.fg,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Spacer(
            Modifier
                .fillMaxWidth()
                .height(2.dp)
                .drawBehind {
                    drawRect(c.line, alpha = glass)
                    val progress = if (scroll.maxValue > 0) scroll.value.toFloat() / scroll.maxValue else 0f
                    if (progress > 0f) drawRect(brand, size = Size(size.width * progress, size.height))
                },
        )
    }
}

@Composable
private fun SourceList(sources: List<StorySource>, onOpen: (String) -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    Column(modifier, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Eyebrow("Sources · ${sources.size}")
        sources.forEachIndexed { i, source ->
            GlassCard(
                Modifier.fillMaxWidth(),
                onClick = { onOpen(source.url) },
                shape = Radius.lg,
                contentPadding = PaddingValues(horizontal = 14.dp, vertical = 12.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Box(
                        Modifier.size(32.dp).clip(Radius.md).background(c.primary.copy(alpha = 0.12f)),
                        contentAlignment = Alignment.Center,
                    ) { Text("${i + 1}", style = ChangeloomTextStyles.code, color = c.primaryText) }
                    Column(Modifier.weight(1f).padding(horizontal = 12.dp)) {
                        Text(source.name, style = MaterialTheme.typography.titleSmall, color = c.fg, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(hostOf(source.url), style = MaterialTheme.typography.bodySmall, color = c.fgSubtle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Icon(Icons.AutoMirrored.Rounded.OpenInNew, contentDescription = "Open ${source.name}", tint = c.fgSubtle, modifier = Modifier.size(18.dp))
                }
            }
        }
    }
}

/** Floating glass pill with Save, Read (only when [read] is known) and Share. */
@Composable
private fun DetailActionBar(saved: Boolean, read: Boolean?, onToggleSave: () -> Unit, onToggleRead: () -> Unit, onShare: () -> Unit) {
    val c = ChangeloomTheme.colors
    Row(
        Modifier
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
            .padding(bottom = 16.dp)
            .shadow(24.dp, Radius.pill, ambientColor = Color.Black.copy(alpha = 0.2f), spotColor = Color.Black.copy(alpha = 0.35f))
            .clip(Radius.pill)
            .background(c.navGlass)
            .border(1.dp, c.lineStrong, Radius.pill)
            .padding(6.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        ActionItem(
            if (saved) Icons.Rounded.Bookmark else Icons.Rounded.BookmarkBorder,
            if (saved) "Saved" else "Save",
            actionLabel = if (saved) "Remove from saved" else "Save",
            active = saved,
            toggle = true,
            onClick = onToggleSave,
        )
        if (read != null) {
            ActionItem(
                if (read) Icons.Rounded.DoneAll else Icons.Rounded.MarkEmailUnread,
                if (read) "Read" else "Unread",
                actionLabel = if (read) "Mark unread" else "Mark read",
                active = read,
                toggle = true,
                onClick = onToggleRead,
            )
        }
        ActionItem(Icons.Rounded.Share, "Share", actionLabel = "Share", active = false, toggle = false, onClick = onShare)
    }
}

@Composable
private fun ActionItem(icon: ImageVector, label: String, actionLabel: String, active: Boolean, toggle: Boolean, onClick: () -> Unit) {
    val c = ChangeloomTheme.colors
    val haptics = LocalHapticFeedback.current
    val fg by animateColorAsState(if (active) c.primaryText else c.fg, expoTween(), label = "actionFg")
    val bg by animateColorAsState(if (active) c.primary.copy(alpha = 0.16f) else Color.Transparent, expoTween(), label = "actionBg")
    Row(
        Modifier
            .clip(Radius.pill)
            .background(bg)
            .clickable(role = Role.Button, onClickLabel = actionLabel) {
                if (toggle) haptics.performHapticFeedback(if (active) HapticFeedbackType.ToggleOff else HapticFeedbackType.ToggleOn)
                onClick()
            }
            .height(44.dp)
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Crossfade(icon, label = "actionIcon") { Icon(it, contentDescription = null, tint = fg, modifier = Modifier.size(20.dp)) }
        Text(label, style = MaterialTheme.typography.labelLarge, color = fg)
    }
}

private fun Story.asSummary() = StorySummary(id, title, summary, kind, severity, importance, publishedAt, topics, isRead, readAt, isBookmarked)

/** "https://www.kotlinlang.org/docs" → "kotlinlang.org"; the raw URL if it won't parse. */
private fun hostOf(url: String): String = try {
    URI(url).host?.removePrefix("www.") ?: url
} catch (e: URISyntaxException) {
    url
}

private fun Context.shareStory(story: Story) {
    val text = listOfNotNull(story.title, story.summary, story.sources.firstOrNull()?.url).joinToString("\n\n")
    val send = Intent(Intent.ACTION_SEND)
        .setType("text/plain")
        .putExtra(Intent.EXTRA_SUBJECT, story.title)
        .putExtra(Intent.EXTRA_TEXT, text)
    startActivity(Intent.createChooser(send, null))
}

private fun previewStory(): Story = previewStories()[0].let {
    Story(
        it.id, it.title, it.summary, it.kind, it.severity, it.importance, it.publishedAt, it.topics,
        isRead = true, bodyMd = PREVIEW_MARKDOWN, isBookmarked = true,
        sources = listOf(
            StorySource("https://kotlinlang.org/docs/whatsnew24.html", "What's new in Kotlin 2.4"),
            StorySource("https://github.com/JetBrains/kotlin/releases", "JetBrains/kotlin releases"),
        ),
    )
}

@Composable
private fun DetailPreview(state: StoryDetailState, placeholder: StorySummary? = null, isRead: Boolean? = true) = StoryDetailContent(
    state, placeholder, isRead, onBack = {}, onRetry = {}, onToggleSave = {}, onToggleRead = {}, onShare = {},
)

@Preview(name = "Detail, dark", heightDp = 1400)
@Composable
private fun DetailDark() = ChangeloomTheme(ThemeMode.Dark) { DetailPreview(StoryDetailState(loading = false, story = previewStory())) }

@Preview(name = "Detail, light, not in timeline", heightDp = 1400)
@Composable
private fun DetailLight() = ChangeloomTheme(ThemeMode.Light) {
    DetailPreview(StoryDetailState(loading = false, story = previewStory().copy(kind = "security", severity = "critical")), isRead = null)
}

@Preview(name = "Detail, loading from feed, dark", heightDp = 900)
@Composable
private fun DetailLoadingDark() = ChangeloomTheme(ThemeMode.Dark) { DetailPreview(StoryDetailState(), placeholder = previewStories()[1]) }

@Preview(name = "Detail, error, light", heightDp = 700)
@Composable
private fun DetailErrorLight() = ChangeloomTheme(ThemeMode.Light) {
    DetailPreview(StoryDetailState(loading = false, error = "Unable to reach changeloom.dev"), isRead = null)
}

private val TOP_BAR_HEIGHT = 56.dp
private val ACTION_BAR_SPACE = 104.dp
private val TITLE_COLLAPSE_OFFSET = 180.dp
private const val LEDE_ENTER_DELAY = 80
private const val META_ENTER_DELAY = 160
private const val BODY_ENTER_DELAY = 220
private const val SOURCES_ENTER_DELAY = 300
private const val BODY_SKELETON_LINES = 7
