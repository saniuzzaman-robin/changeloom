package dev.changeloom.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.shared.data.TopicRequest

/** Profile section: ask for a topic that isn't in the catalog, and see how earlier requests were handled. */
@Composable
internal fun TopicRequestSection(state: ProfileState, onText: (String) -> Unit, onSubmit: () -> Unit) {
    val c = ChangeloomTheme.colors
    val canSubmit = state.requestText.trim().length >= REQUEST_MIN_LENGTH && !state.submittingRequest
    Column {
        Eyebrow(stringResource(R.string.request_title))
        Spacer(Modifier.height(4.dp))
        Text(
            stringResource(R.string.request_body),
            style = MaterialTheme.typography.bodyMedium,
            color = c.fgMuted,
        )
        Spacer(Modifier.height(12.dp))
        ChangeloomTextField(
            value = state.requestText,
            onValueChange = { if (it.length <= REQUEST_MAX_LENGTH) onText(it) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !state.submittingRequest,
            placeholder = stringResource(R.string.request_placeholder),
            isError = state.requestError != null,
            supportingText = state.requestError,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
            keyboardActions = KeyboardActions(onSend = { if (canSubmit) onSubmit() }),
        )
        Spacer(Modifier.height(12.dp))
        PrimaryButton(
            stringResource(R.string.request_send),
            onClick = onSubmit,
            modifier = Modifier.fillMaxWidth(),
            enabled = canSubmit || state.submittingRequest,
            loading = state.submittingRequest,
        )
        val requests = state.requests.orEmpty()
        if (requests.isNotEmpty()) {
            Spacer(Modifier.height(20.dp))
            Eyebrow(stringResource(R.string.request_list_title))
            Spacer(Modifier.height(8.dp))
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) { requests.forEach { RequestRow(it) } }
        }
    }
}

@Composable
private fun RequestRow(request: TopicRequest) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    val (label, color) = when (request.status) {
        "accepted" -> R.string.request_added to c.success
        "merged" -> R.string.request_covered to c.success
        "rejected" -> R.string.request_declined to c.rose
        else -> R.string.request_pending to c.fgMuted
    }
    Column(Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                request.text,
                Modifier.weight(1f),
                style = MaterialTheme.typography.bodyMedium,
                color = c.fg,
                maxLines = 1,
            )
            StatusChip(stringResource(label), color)
        }
        val detail = listOfNotNull(request.topic?.let { stringResource(R.string.request_topic, topicName(it)) }, request.note).joinToString(" · ")
        if (detail.isNotEmpty()) {
            Text(detail, style = MaterialTheme.typography.bodySmall, color = c.fgMuted)
        }
    }
}

@Composable
private fun StatusChip(label: String, color: Color) {
    Eyebrow(
        label,
        Modifier
            .clip(Radius.pill)
            .background(color.copy(alpha = 0.12f))
            .border(1.dp, color.copy(alpha = 0.28f), Radius.pill)
            .padding(horizontal = 9.dp, vertical = 3.dp),
        color = color,
    )
}
