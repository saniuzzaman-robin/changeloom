package dev.changeloom.android.ui

import androidx.activity.compose.LocalActivity
import androidx.annotation.StringRes
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Logout
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.DarkMode
import androidx.compose.material.icons.rounded.DeleteForever
import androidx.compose.material.icons.rounded.DoneAll
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.LightMode
import androidx.compose.material.icons.rounded.Lock
import androidx.compose.material.icons.rounded.PrivacyTip
import androidx.compose.material.icons.rounded.SettingsBrightness
import androidx.compose.material.icons.rounded.Tag
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.BuildConfig
import dev.changeloom.android.R
import dev.changeloom.android.ads.ConsentManager
import dev.changeloom.android.auth.requestGoogleIdToken
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientAvatar
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.shared.data.MeStats
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.util.Locale
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject
import kotlin.math.roundToLong

@Composable
internal fun ProfileScreen(
    onEditTopics: () -> Unit,
    contentPadding: PaddingValues,
    vm: ProfileViewModel = koinViewModel(),
    picker: TopicPickerViewModel = koinViewModel(),
    consent: ConsentManager = koinInject(),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val privacyOptions by consent.privacyOptionsRequired.collectAsStateWithLifecycle()
    val activity = LocalActivity.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val mode by vm.themeMode.collectAsStateWithLifecycle()
    val pickerState = picker.state.collectAsStateWithLifecycle().value
    val followed = pickerState.followed
    val professionNames = remember(pickerState.professions, pickerState.savedProfessions) {
        val byslug = pickerState.professions.associate { it.slug to it.name }
        pickerState.savedProfessions.mapNotNull { byslug[it] }
    }
    val photo = vm.photo.collectAsStateWithLifecycle().value
    val photoImage = remember(photo) { photo?.asImageBitmap() }
    LaunchedEffect(Unit) { vm.refresh() }
    ProfileContent(
        displayName = vm.displayName,
        email = vm.email,
        photo = photoImage,
        state = state,
        followed = followed,
        professions = professionNames,
        themeMode = mode,
        version = BuildConfig.VERSION_NAME,
        contentPadding = contentPadding,
        onEditTopics = onEditTopics,
        onCountry = vm::setCountry,
        onEditProfessions = {
            picker.setStep(PickerStep.Professions)
            onEditTopics()
        },
        onThemeMode = vm::setThemeMode,
        onRetry = vm::refresh,
        onRequestText = vm::setRequestText,
        onSubmitRequest = vm::submitRequest,
        onSignOut = vm::signOut,
        onPrivacyOptions = activity?.takeIf { privacyOptions }?.let { { consent.showPrivacyOptions(it) } },
        deleteActions = DeleteActions(
            start = vm::startDelete,
            cancel = vm::cancelDelete,
            confirm = vm::confirmDelete,
            withPassword = vm::deleteWithPassword,
            withGoogle = {
                scope.launch {
                    try {
                        vm.deleteWithGoogle(requestGoogleIdToken(context, webClientId(context)))
                    } catch (e: GetCredentialCancellationException) {
                        // Dismissed the account picker: the dialog stays up.
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        vm.googleReauthFailed(e)
                    }
                }
            },
        ),
    )
}

/** What the account-deletion dialogs call back into. */
internal class DeleteActions(
    val start: () -> Unit = {},
    val cancel: () -> Unit = {},
    val confirm: () -> Unit = {},
    val withPassword: (String) -> Unit = {},
    val withGoogle: () -> Unit = {},
)

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ProfileContent(
    displayName: String?,
    email: String?,
    photo: ImageBitmap?,
    state: ProfileState,
    followed: List<String>,
    professions: List<String> = emptyList(),
    onEditProfessions: () -> Unit = {},
    onCountry: (String) -> Unit = {},
    themeMode: ThemeMode,
    version: String,
    contentPadding: PaddingValues,
    onEditTopics: () -> Unit,
    onThemeMode: (ThemeMode) -> Unit,
    onRetry: () -> Unit,
    onRequestText: (String) -> Unit,
    onSubmitRequest: () -> Unit,
    onSignOut: () -> Unit,
    onPrivacyOptions: (() -> Unit)? = null,
    deleteActions: DeleteActions = DeleteActions(),
) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    var confirmSignOut by rememberSaveable { mutableStateOf(false) }
    var pickCountry by rememberSaveable { mutableStateOf(false) }
    val scroll = rememberScrollState()

    Box(Modifier.fillMaxSize().tabContentBounds(contentPadding)) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(scroll)
                .padding(bottom = 24.dp),
        ) {
            ProfileBanner(displayName, email, photo)
            Column(
                Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)).padding(horizontal = Spacing.gutter),
                verticalArrangement = Arrangement.spacedBy(28.dp),
            ) {
                Column(Modifier.enter(160)) {
                    Eyebrow(stringResource(R.string.profile_activity))
                    Spacer(Modifier.height(12.dp))
                    StatusBanner(state.error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error, actionLabel = stringResource(R.string.retry), onAction = onRetry)
                    if (state.error != null) Spacer(Modifier.height(12.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        StatTile(stringResource(R.string.saved), state.stats?.saved, Icons.Rounded.Bookmark, c.primaryText, Modifier.weight(1f))
                        StatTile(stringResource(R.string.read), state.stats?.read, Icons.Rounded.DoneAll, c.success, Modifier.weight(1f))
                        StatTile(stringResource(R.string.topics), followed.size.toLong(), Icons.Rounded.Tag, c.accent, Modifier.weight(1f))
                    }
                }

                Column(Modifier.enter(240)) {
                    if (professions.isNotEmpty()) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Eyebrow(stringResource(R.string.professions_label), Modifier.weight(1f))
                            TextAction(stringResource(R.string.edit_professions), onEditProfessions, icon = Icons.Rounded.Edit)
                        }
                        Spacer(Modifier.height(4.dp))
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            professions.forEach { TopicChip(it) }
                        }
                        Spacer(Modifier.height(20.dp))
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Eyebrow(stringResource(R.string.following), Modifier.weight(1f))
                        TextAction(stringResource(R.string.edit_topics), onEditTopics, icon = Icons.Rounded.Edit)
                    }
                    Spacer(Modifier.height(4.dp))
                    if (followed.isEmpty()) {
                        Text(stringResource(R.string.following_none), style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
                    } else {
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            followed.forEach { TopicChip(topicName(it)) }
                        }
                    }
                }

                Column(Modifier.enter(260)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Eyebrow(stringResource(R.string.country_label), Modifier.weight(1f))
                        TextAction(stringResource(R.string.edit_country), { pickCountry = true }, icon = Icons.Rounded.Edit)
                    }
                    Spacer(Modifier.height(4.dp))
                    Text(
                        state.country?.let(::countryName) ?: stringResource(R.string.country_none),
                        style = MaterialTheme.typography.bodyMedium,
                        color = if (state.country == null) c.fgMuted else c.fg,
                    )
                }

                Column(Modifier.enter(280)) {
                    TopicRequestSection(state, onRequestText, onSubmitRequest)
                }

                Column(Modifier.enter(320)) {
                    Eyebrow(stringResource(R.string.appearance))
                    Spacer(Modifier.height(12.dp))
                    ThemeSwitch(themeMode, onThemeMode)
                }

                Column(Modifier.enter(400), horizontalAlignment = Alignment.CenterHorizontally) {
                    // Only where the consent rules let users change their ad choices (EEA, UK, ...).
                    if (onPrivacyOptions != null) {
                        SecondaryButton(
                            stringResource(R.string.privacy_options),
                            onClick = onPrivacyOptions,
                            modifier = Modifier.fillMaxWidth(),
                            icon = Icons.Rounded.PrivacyTip,
                        )
                        Spacer(Modifier.height(12.dp))
                    }
                    SecondaryButton(
                        stringResource(R.string.sign_out),
                        onClick = { confirmSignOut = true },
                        modifier = Modifier.fillMaxWidth(),
                        icon = Icons.AutoMirrored.Rounded.Logout,
                        contentColor = c.rose,
                    )
                    Spacer(Modifier.height(8.dp))
                    TextAction(stringResource(R.string.delete_account), deleteActions.start, icon = Icons.Rounded.DeleteForever, color = c.red)
                    Spacer(Modifier.height(16.dp))
                    Eyebrow(stringResource(R.string.app_version, version))
                }
            }
        }
    }

    if (pickCountry) {
        val countries = remember { Locale.getISOCountries().map { it to countryName(it) }.sortedBy { it.second } }
        AlertDialog(
            onDismissRequest = { pickCountry = false },
            confirmButton = {},
            dismissButton = { TextAction(stringResource(R.string.cancel), onClick = { pickCountry = false }, color = c.fgMuted) },
            title = { Text(stringResource(R.string.country_title)) },
            text = {
                Column {
                    Text(stringResource(R.string.country_body), style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
                    Spacer(Modifier.height(8.dp))
                    LazyColumn {
                        items(countries, key = { it.first }) { (code, name) ->
                            TextAction(
                                name,
                                onClick = {
                                    pickCountry = false
                                    onCountry(code)
                                },
                                modifier = Modifier.fillMaxWidth(),
                                color = if (code == state.country) c.primaryText else c.fg,
                            )
                        }
                    }
                }
            },
            shape = Radius.xxl,
            containerColor = c.elevated,
            titleContentColor = c.fg,
            textContentColor = c.fgMuted,
        )
    }
    if (confirmSignOut) {
        AlertDialog(
            onDismissRequest = { confirmSignOut = false },
            confirmButton = {
                TextAction(stringResource(R.string.sign_out), onClick = {
                    confirmSignOut = false
                    onSignOut()
                }, color = c.rose)
            },
            dismissButton = { TextAction(stringResource(R.string.cancel), onClick = { confirmSignOut = false }, color = c.fgMuted) },
            title = { Text(stringResource(R.string.sign_out_title)) },
            text = { Text(stringResource(R.string.sign_out_body)) },
            shape = Radius.xxl,
            containerColor = c.elevated,
            titleContentColor = c.fg,
            textContentColor = c.fgMuted,
        )
    }
    state.delete?.let { DeleteAccountDialog(it, deleteActions) }
}

@Composable
private fun DeleteAccountDialog(delete: DeleteState, actions: DeleteActions) {
    val c = ChangeloomTheme.colors
    var password by remember(delete.step) { mutableStateOf("") }
    val deleting = delete.step == DeleteStep.Deleting
    AlertDialog(
        onDismissRequest = actions.cancel,
        properties = DialogProperties(dismissOnBackPress = !deleting, dismissOnClickOutside = !deleting),
        confirmButton = {
            when (delete.step) {
                DeleteStep.Confirm -> TextAction(stringResource(R.string.delete), actions.confirm, color = c.red)
                DeleteStep.Password -> TextAction(
                    stringResource(R.string.delete),
                    onClick = { actions.withPassword(password) },
                    enabled = password.isNotEmpty(),
                    color = c.red,
                )
                DeleteStep.Google -> TextAction(stringResource(R.string.continue_with_google), actions.withGoogle, color = c.red)
                DeleteStep.Deleting -> Unit
            }
        },
        dismissButton = {
            if (!deleting) TextAction(stringResource(R.string.cancel), onClick = actions.cancel, color = c.fgMuted)
        },
        title = {
            Text(stringResource(if (delete.step == DeleteStep.Confirm || deleting) R.string.delete_title else R.string.reauth_title))
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    stringResource(
                        when (delete.step) {
                            DeleteStep.Confirm -> R.string.delete_body
                            DeleteStep.Password -> R.string.reauth_password_body
                            DeleteStep.Google -> R.string.reauth_google_body
                            DeleteStep.Deleting -> R.string.deleting
                        },
                    ),
                )
                if (delete.step == DeleteStep.Password) {
                    ChangeloomTextField(
                        value = password,
                        onValueChange = { password = it },
                        label = stringResource(R.string.password),
                        leadingIcon = Icons.Rounded.Lock,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                        keyboardActions = KeyboardActions(onDone = { if (password.isNotEmpty()) actions.withPassword(password) }),
                        visualTransformation = PasswordVisualTransformation(),
                    )
                }
                if (deleting) LinearProgressIndicator(Modifier.fillMaxWidth(), color = c.red, trackColor = c.surface2)
                StatusBanner(delete.error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error)
            }
        },
        shape = Radius.xxl,
        containerColor = c.elevated,
        titleContentColor = c.fg,
        textContentColor = c.fgMuted,
    )
}

@Composable
private fun ProfileBanner(displayName: String?, email: String?, photo: ImageBitmap?) {
    val c = ChangeloomTheme.colors
    val name = displayName ?: email?.let(::firstNameOf) ?: stringResource(R.string.reader_fallback_name)
    Box(Modifier.fillMaxWidth()) {
        Column(
            Modifier
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal))
                .padding(start = Spacing.gutter, end = Spacing.gutter, top = 32.dp, bottom = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            GradientAvatar(displayName ?: email ?: name, Modifier.enter(0), size = 88.dp, photo = photo)
            Spacer(Modifier.height(16.dp))
            Column(Modifier.enter(80), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    name,
                    style = MaterialTheme.typography.headlineMedium,
                    color = c.fg,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    textAlign = TextAlign.Center,
                )
                if (email != null) {
                    Spacer(Modifier.height(4.dp))
                    Text(email, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/** A count that ticks up from zero the first time it is known; "–" while it loads. */
@Composable
private fun StatTile(label: String, value: Long?, icon: ImageVector, tint: Color, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val inspection = LocalInspectionMode.current
    val shown = remember { Animatable(if (inspection) (value ?: 0).toFloat() else 0f) }
    LaunchedEffect(value) { if (value != null) shown.animateTo(value.toFloat(), expoTween(Durations.SLOW + 600)) }
    GlassCard(modifier, contentPadding = PaddingValues(14.dp)) {
        Box(
            Modifier
                .size(32.dp)
                .clip(Radius.md)
                .background(tint.copy(alpha = 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = null, Modifier.size(18.dp), tint = tint)
        }
        Spacer(Modifier.height(12.dp))
        Text(
            if (value == null) "–" else shown.value.roundToLong().toString(),
            style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.Bold),
            color = c.fg,
            maxLines = 1,
        )
        Eyebrow(label)
    }
}

/** Segmented System / Light / Dark switch; a washed pill slides under the selected option. */
@Composable
private fun ThemeSwitch(mode: ThemeMode, onChange: (ThemeMode) -> Unit) {
    val c = ChangeloomTheme.colors
    val options = ThemeMode.entries
    BoxWithConstraints(
        Modifier
            .fillMaxWidth()
            .clip(Radius.pill)
            .background(c.surface2)
            .border(1.dp, c.line, Radius.pill)
            .padding(4.dp),
    ) {
        val segment = maxWidth / options.size
        val indicator by animateDpAsState(segment * mode.ordinal, expoTween(Durations.SLOW), label = "themeIndicator")
        Box(
            Modifier
                .offset(x = indicator)
                .width(segment)
                .height(SEGMENT_HEIGHT)
                .clip(Radius.pill)
                .background(c.surface)
                .background(ChangeloomTheme.gradients.wash)
                .border(1.dp, c.primary.copy(alpha = 0.4f), Radius.pill),
        )
        Row(Modifier.selectableGroup()) {
            options.forEach { option ->
                val selected = option == mode
                val fg by animateColorAsState(if (selected) c.primaryText else c.fgMuted, expoTween(), label = "themeFg")
                Row(
                    Modifier
                        .weight(1f)
                        .height(SEGMENT_HEIGHT)
                        .clip(Radius.pill)
                        .selectable(selected, role = Role.RadioButton) { onChange(option) },
                    horizontalArrangement = Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(option.icon(), contentDescription = null, Modifier.size(18.dp), tint = fg)
                    Spacer(Modifier.width(8.dp))
                    Text(stringResource(option.label()), style = MaterialTheme.typography.labelLarge, color = fg)
                }
            }
        }
    }
}

@StringRes
private fun ThemeMode.label(): Int = when (this) {
    ThemeMode.System -> R.string.theme_system
    ThemeMode.Light -> R.string.theme_light
    ThemeMode.Dark -> R.string.theme_dark
}

private fun ThemeMode.icon(): ImageVector = when (this) {
    ThemeMode.System -> Icons.Rounded.SettingsBrightness
    ThemeMode.Light -> Icons.Rounded.LightMode
    ThemeMode.Dark -> Icons.Rounded.DarkMode
}

@Composable
private fun ProfilePreview(state: ProfileState, mode: ThemeMode) {
    ProfileContent(
        displayName = "Ada Lovelace",
        email = "ada@example.com",
        photo = null,
        state = state,
        followed = listOf("languages/kotlin", "mobile/android", "security", "cloud/aws"),
        themeMode = mode,
        version = "0.1.0",
        contentPadding = PaddingValues(0.dp),
        onEditTopics = {},
        onThemeMode = {},
        onRetry = {},
        onRequestText = {},
        onSubmitRequest = {},
        onSignOut = {},
    )
}

@Preview(name = "Profile, dark", heightDp = 1000)
@Composable
private fun ProfileDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { ProfilePreview(ProfileState(stats = MeStats(saved = 12, read = 148)), ThemeMode.Dark) }
}

@Preview(name = "Profile, error, light", heightDp = 1000)
@Composable
private fun ProfileErrorLight() = ChangeloomTheme(ThemeMode.Light) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { ProfilePreview(ProfileState(error = "Couldn't reach the server"), ThemeMode.Light) }
}

private val SEGMENT_HEIGHT = 40.dp

private fun countryName(code: String): String = Locale("", code).displayCountry
