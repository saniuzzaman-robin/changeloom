package dev.changeloom.android.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.AlternateEmail
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.Lock
import androidx.compose.material.icons.rounded.Visibility
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.auth.requestGoogleIdToken
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientText
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.WordRiseText
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTextStyles
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

/** Firebase rejects shorter passwords; shown as a hint when creating an account. */
private const val MIN_PASSWORD_LENGTH = 6
private const val ORB_PERIOD_MS = 24_000
private const val CHIP_PERIOD_MS = 7_000

private enum class AuthMode(val label: String) { SignIn("Sign in"), Register("Create account") }

@Composable
fun SignInScreen(vm: SignInViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    SignInContent(
        state = state,
        onSignIn = vm::signIn,
        onRegister = vm::register,
        onGoogle = {
            scope.launch {
                try {
                    vm.googleToken(requestGoogleIdToken(context, webClientId(context)))
                } catch (e: GetCredentialCancellationException) {
                    // User dismissed the account picker.
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    vm.fail(e.message ?: "Google sign-in failed")
                }
            }
        },
    )
}

@Composable
private fun SignInContent(
    state: SignInState,
    onSignIn: (String, String) -> Unit,
    onRegister: (String, String) -> Unit,
    onGoogle: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val focus = LocalFocusManager.current
    var mode by rememberSaveable { mutableStateOf(AuthMode.SignIn) }
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var showPassword by rememberSaveable { mutableStateOf(false) }
    val filled = email.isNotBlank() && password.isNotEmpty()
    val submit = {
        focus.clearFocus()
        if (mode == AuthMode.SignIn) onSignIn(email, password) else onRegister(email, password)
    }

    // Shake the card whenever a new error arrives (the view model clears it at the start of each attempt).
    val shake = remember { Animatable(0f) }
    val shakeDistance = with(LocalDensity.current) { 10.dp.toPx() }
    LaunchedEffect(state.error) {
        if (state.error != null) {
            for (step in listOf(-1f, 1f, -0.6f, 0.6f, -0.3f, 0f)) shake.animateTo(step * shakeDistance, tween(55))
        }
    }

    Box(Modifier.fillMaxSize().background(c.bg)) {
        DriftingOrbs(Modifier.matchParentSize())
        GridBackground(Modifier.matchParentSize(), fadeCenter = Offset(0.5f, 0.25f))
        SpotlightGlow(Modifier.matchParentSize())
        FloatingChangelogChips(Modifier.matchParentSize())
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .padding(horizontal = 24.dp, vertical = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            LoomMark(Modifier.size(76.dp), animate = !LocalInspectionMode.current)
            Spacer(Modifier.height(20.dp))
            GradientText("Changeloom", Modifier.enter(delayMillis = 350), style = MaterialTheme.typography.displayMedium)
            Spacer(Modifier.height(10.dp))
            WordRiseText(
                "Every release that matters, woven into one feed.",
                Modifier.widthIn(max = 340.dp),
                style = MaterialTheme.typography.titleMedium,
                color = c.fgMuted,
                startDelayMillis = 600,
                horizontalArrangement = Arrangement.Center,
            )
            Spacer(Modifier.height(32.dp))
            GlassCard(
                Modifier
                    .widthIn(max = 440.dp)
                    .fillMaxWidth()
                    .enter(delayMillis = 900)
                    .graphicsLayer { translationX = shake.value },
                shape = Radius.xxl,
                contentPadding = PaddingValues(20.dp),
            ) {
                ModeToggle(mode, onChange = { mode = it }, enabled = !state.busy)
                Spacer(Modifier.height(20.dp))
                AuthField(
                    value = email,
                    onValueChange = { email = it },
                    label = "Email",
                    icon = Icons.Rounded.AlternateEmail,
                    enabled = !state.busy,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
                )
                Spacer(Modifier.height(12.dp))
                AuthField(
                    value = password,
                    onValueChange = { password = it },
                    label = "Password",
                    icon = Icons.Rounded.Lock,
                    enabled = !state.busy,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { if (filled && !state.busy) submit() }),
                    visualTransformation = if (showPassword) VisualTransformation.None else PasswordVisualTransformation(),
                    supportingText = if (mode == AuthMode.Register) "At least $MIN_PASSWORD_LENGTH characters" else null,
                    trailingIcon = {
                        IconButton(onClick = { showPassword = !showPassword }) {
                            Icon(
                                if (showPassword) Icons.Rounded.VisibilityOff else Icons.Rounded.Visibility,
                                contentDescription = if (showPassword) "Hide password" else "Show password",
                            )
                        }
                    },
                )
                StatusBanner(
                    state.error,
                    Icons.Rounded.ErrorOutline,
                    Modifier.padding(top = 12.dp),
                    tone = BannerTone.Error,
                )
                Spacer(Modifier.height(16.dp))
                PrimaryButton(
                    text = mode.label,
                    onClick = submit,
                    modifier = Modifier.fillMaxWidth(),
                    enabled = filled,
                    loading = state.busy,
                )
                Row(Modifier.padding(vertical = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    HorizontalDivider(Modifier.weight(1f), color = c.line)
                    Eyebrow("or", Modifier.padding(horizontal = 12.dp))
                    HorizontalDivider(Modifier.weight(1f), color = c.line)
                }
                SecondaryButton(
                    text = "Continue with Google",
                    onClick = onGoogle,
                    modifier = Modifier.fillMaxWidth(),
                    enabled = !state.busy,
                    leading = { GoogleMark(Modifier.size(18.dp)) },
                )
            }
        }
    }
}

@Composable
private fun ModeToggle(mode: AuthMode, onChange: (AuthMode) -> Unit, enabled: Boolean) {
    val c = ChangeloomTheme.colors
    BoxWithConstraints(
        Modifier
            .fillMaxWidth()
            .clip(Radius.lg)
            .background(c.surface2)
            .border(1.dp, c.line, Radius.lg)
            .padding(4.dp),
    ) {
        val half = maxWidth / 2
        val indicator by animateDpAsState(
            if (mode == AuthMode.SignIn) 0.dp else half,
            expoTween(Durations.SLOW),
            label = "modeIndicator",
        )
        Box(
            Modifier
                .offset(x = indicator)
                .width(half)
                .height(40.dp)
                .clip(Radius.md)
                .background(ChangeloomTheme.gradients.button),
        )
        Row(Modifier.selectableGroup()) {
            AuthMode.entries.forEach { m ->
                val selected = m == mode
                val color by animateColorAsState(if (selected) c.primaryFg else c.fgMuted, expoTween(), label = "modeLabel")
                Box(
                    Modifier
                        .weight(1f)
                        .height(40.dp)
                        .clip(Radius.md)
                        .selectable(selected, enabled = enabled, role = Role.Tab, onClick = { onChange(m) }),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(m.label, style = MaterialTheme.typography.labelLarge, color = color)
                }
            }
        }
    }
}

@Composable
private fun AuthField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    icon: ImageVector,
    enabled: Boolean,
    keyboardOptions: KeyboardOptions,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    supportingText: String? = null,
    trailingIcon: (@Composable () -> Unit)? = null,
) {
    val c = ChangeloomTheme.colors
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier.fillMaxWidth(),
        enabled = enabled,
        label = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null, modifier = Modifier.size(20.dp)) },
        trailingIcon = trailingIcon,
        supportingText = supportingText?.let { { Text(it) } },
        visualTransformation = visualTransformation,
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        singleLine = true,
        shape = Radius.lg,
        colors = OutlinedTextFieldDefaults.colors(
            focusedContainerColor = c.surface2,
            unfocusedContainerColor = c.surface2,
            disabledContainerColor = c.surface2,
            focusedBorderColor = c.primary,
            unfocusedBorderColor = c.lineStrong,
            focusedLabelColor = c.primaryText,
            focusedLeadingIconColor = c.primaryText,
            unfocusedLeadingIconColor = c.fgSubtle,
            cursorColor = c.primary,
        ),
    )
}

private class Orb(val color: Color, val x: Float, val y: Float, val phase: Float)

/** Large soft colour fields drifting slowly behind everything, like the site's hero. */
@Composable
private fun DriftingOrbs(modifier: Modifier) {
    val c = ChangeloomTheme.colors
    val alpha = if (c.isDark) 0.30f else 0.14f
    val orbs = remember(c) {
        listOf(Orb(c.primary, 0.15f, 0.10f, 0f), Orb(c.accent, 0.92f, 0.38f, 2.1f), Orb(c.blue, 0.25f, 0.88f, 4.2f))
    }
    val phase by rememberInfiniteTransition(label = "orbs").animateFloat(
        0f,
        2 * PI.toFloat(),
        infiniteRepeatable(tween(ORB_PERIOD_MS, easing = LinearEasing)),
        label = "orbPhase",
    )
    Spacer(
        modifier.drawBehind {
            val radius = size.minDimension * 0.8f
            orbs.forEach { o ->
                val center = Offset(
                    size.width * (o.x + 0.10f * cos(phase + o.phase)),
                    size.height * (o.y + 0.05f * sin(phase + o.phase)),
                )
                drawCircle(Brush.radialGradient(listOf(o.color.copy(alpha = alpha), Color.Transparent), center, radius), radius, center)
            }
        },
    )
}

private class FloatingChip(val text: String, val color: Color, val x: Float, val y: Float)

/** Faint changelog fragments bobbing around the edges of the screen. */
@Composable
private fun FloatingChangelogChips(modifier: Modifier) {
    val c = ChangeloomTheme.colors
    val chips = remember(c) {
        listOf(
            FloatingChip("v2.4.0", c.primaryText, 0.06f, 0.06f),
            FloatingChip("+ added", c.success, 0.66f, 0.10f),
            FloatingChip("deprecated", c.amber, 0.04f, 0.28f),
            FloatingChip("BREAKING", c.rose, 0.70f, 0.31f),
            FloatingChip("fix: retry on 503", c.fgMuted, 0.06f, 0.91f),
            FloatingChip("kotlin 2.2.0", c.accent, 0.62f, 0.95f),
        )
    }
    val phase by rememberInfiniteTransition(label = "chips").animateFloat(
        0f,
        2 * PI.toFloat(),
        infiniteRepeatable(tween(CHIP_PERIOD_MS, easing = LinearEasing)),
        label = "chipPhase",
    )
    val bob = with(LocalDensity.current) { 6.dp.toPx() }
    BoxWithConstraints(modifier) {
        chips.forEachIndexed { i, chip ->
            Text(
                chip.text,
                style = ChangeloomTextStyles.eyebrow,
                color = chip.color,
                modifier = Modifier
                    .offset(x = maxWidth * chip.x, y = maxHeight * chip.y)
                    .enter(delayMillis = 1200 + i * 120)
                    .graphicsLayer {
                        translationY = sin(phase + i * 1.3f) * bob
                        alpha = 0.6f
                    }
                    .clip(Radius.pill)
                    .background(chip.color.copy(alpha = 0.08f))
                    .border(1.dp, chip.color.copy(alpha = 0.22f), Radius.pill)
                    .padding(horizontal = 10.dp, vertical = 4.dp),
            )
        }
    }
}

private val GoogleBlue = Color(0xFF4285F4)
private val GoogleGreen = Color(0xFF34A853)
private val GoogleYellow = Color(0xFFFBBC05)
private val GoogleRed = Color(0xFFEA4335)

/** The four-colour Google "G", drawn so no image asset is needed. */
@Composable
private fun GoogleMark(modifier: Modifier = Modifier) {
    Canvas(modifier) {
        val stroke = size.minDimension * 0.22f
        val topLeft = Offset(stroke / 2, stroke / 2)
        val arcSize = Size(size.width - stroke, size.height - stroke)
        fun arc(color: Color, start: Float, sweep: Float) =
            drawArc(color, start, sweep, useCenter = false, topLeft = topLeft, size = arcSize, style = Stroke(stroke))
        arc(GoogleBlue, 0f, 50f)
        arc(GoogleGreen, 50f, 90f)
        arc(GoogleYellow, 140f, 75f)
        arc(GoogleRed, 215f, 105f)
        drawRect(GoogleBlue, Offset(size.width / 2, size.height / 2 - stroke / 2), Size(size.width / 2, stroke))
    }
}

/** Generated by the google-services plugin only when the Firebase config has a Google OAuth client. */
private fun webClientId(context: android.content.Context): String {
    val id = context.resources.getIdentifier("default_web_client_id", "string", context.packageName)
    check(id != 0) {
        "Google sign-in is not configured: enable the Google provider and add your SHA-1 in Firebase, then re-download google-services.json"
    }
    return context.getString(id)
}

@Preview(name = "Dark", heightDp = 860)
@Composable
private fun SignInDark() = ChangeloomTheme(ThemeMode.Dark) {
    SignInContent(SignInState(), onSignIn = { _, _ -> }, onRegister = { _, _ -> }, onGoogle = {})
}

@Preview(name = "Light, error", heightDp = 860)
@Composable
private fun SignInLight() = ChangeloomTheme(ThemeMode.Light) {
    SignInContent(
        SignInState(error = "The supplied auth credential is incorrect."),
        onSignIn = { _, _ -> },
        onRegister = { _, _ -> },
        onGoogle = {},
    )
}
