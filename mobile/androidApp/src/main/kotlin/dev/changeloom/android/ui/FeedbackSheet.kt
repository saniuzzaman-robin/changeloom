package dev.changeloom.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Star
import androidx.compose.material.icons.rounded.StarBorder
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.ChangeloomSheet
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.theme.ChangeloomTheme

private const val MAX_RATING = 5

/** What the feedback sheet calls back into. */
internal class FeedbackActions(
    val text: (String) -> Unit = {},
    val category: (String) -> Unit = {},
    val rating: (Int) -> Unit = {},
    val send: () -> Unit = {},
    /** The sheet closed; a sent form starts over next time, a draft is kept. */
    val dismiss: () -> Unit = {},
)

/** Sheet from the profile: send feedback or a review (category, optional stars, a message), then a thank-you. */
@Composable
internal fun FeedbackSheet(state: FeedbackState, actions: FeedbackActions, onClose: () -> Unit) {
    val close = {
        onClose()
        actions.dismiss()
    }
    ChangeloomSheet(
        close,
        stringResource(if (state.sent) R.string.feedback_thanks_title else R.string.feedback_title),
        stringResource(if (state.sent) R.string.feedback_thanks else R.string.feedback_body),
    ) { hide ->
        if (state.sent) {
            PrimaryButton(stringResource(R.string.feedback_done), onClick = hide, modifier = Modifier.fillMaxWidth())
        } else {
            FeedbackForm(state, actions)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FeedbackForm(state: FeedbackState, actions: FeedbackActions) {
    val c = ChangeloomTheme.colors
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        FEEDBACK_CATEGORIES.forEach { category ->
            TopicChip(
                stringResource(categoryLabel(category)),
                selected = state.category == category,
                onClick = { actions.category(category) },
            )
        }
    }
    Spacer(Modifier.height(8.dp))
    Row {
        for (star in 1..MAX_RATING) {
            val filled = star <= (state.rating ?: 0)
            IconButton(onClick = { actions.rating(star) }, enabled = !state.sending) {
                Icon(
                    if (filled) Icons.Rounded.Star else Icons.Rounded.StarBorder,
                    contentDescription = stringResource(R.string.feedback_star, star),
                    tint = if (filled) c.primaryText else c.fgSubtle,
                )
            }
        }
    }
    Spacer(Modifier.height(8.dp))
    ChangeloomTextField(
        value = state.text,
        onValueChange = { if (it.length <= FEEDBACK_MAX_LENGTH) actions.text(it) },
        modifier = Modifier.fillMaxWidth(),
        enabled = !state.sending,
        placeholder = stringResource(R.string.feedback_placeholder),
        isError = state.error != null,
        supportingText = state.error,
        minLines = 4,
    )
    Spacer(Modifier.height(12.dp))
    PrimaryButton(
        stringResource(R.string.feedback_send),
        onClick = actions.send,
        modifier = Modifier.fillMaxWidth(),
        enabled = state.text.trim().length >= FEEDBACK_MIN_LENGTH || state.sending,
        loading = state.sending,
    )
}

private fun categoryLabel(category: String): Int = when (category) {
    "bug" -> R.string.feedback_category_bug
    "praise" -> R.string.feedback_category_praise
    "other" -> R.string.feedback_category_other
    else -> R.string.feedback_category_idea
}
