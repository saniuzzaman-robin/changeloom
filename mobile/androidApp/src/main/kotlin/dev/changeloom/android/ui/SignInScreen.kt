package dev.changeloom.android.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.AlternateEmail
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.Lock
import androidx.compose.material.icons.rounded.Visibility
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.auth.requestGoogleIdToken
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GradientText
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.components.WordRiseText
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
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
private val SHEET_MAX_WIDTH = 520.dp

private enum class AuthMode(val action: String, val title: String, val subtitle: String, val switchPrompt: String) {
    SignIn("Sign in", "Welcome back", "Pick up your feed where you left off.", "New here?"),
    Register("Create account", "Create your account", "It takes a few seconds. You'll pick your topics next.", "Have an account?");

    val other: AuthMode get() = if (this == SignIn) Register else SignIn
}

@Composable
fun SignInScreen(vm: SignInViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // A second credential request while the account picker is up makes the system cancel the first one
    // ("Cancelled by Changeloom"), so the form stays busy until the picker returns.
    var picking by remember { mutableStateOf(false) }
    SignInContent(
        state = if (picking) state.copy(busy = true) else state,
        onSignIn = vm::signIn,
        onRegister = vm::register,
        onGoogle = {
            if (!picking) {
                picking = true
                scope.launch {
                    try {
                        vm.googleToken(requestGoogleIdToken(context, webClientId(context)))
                    } catch (e: GetCredentialCancellationException) {
                        // User dismissed the account picker.
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        vm.fail(e.message ?: "Google sign-in failed")
                    } finally {
                        picking = false
                    }
                }
            }
        },
    )
}

/** Brand hero centred in the space above a bottom-anchored form sheet; scrolls once the keyboard leaves too little room. */
@Composable
private fun SignInContent(
    state: SignInState,
    onSignIn: (String, String) -> Unit,
    onRegister: (String, String) -> Unit,
    onGoogle: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    Box(Modifier.fillMaxSize().background(c.bg)) {
        DriftingOrbs(Modifier.matchParentSize())
        GridBackground(Modifier.fillMaxWidth().fillMaxHeight(0.6f), fadeCenter = Offset(0.5f, 0.3f), fadeRadius = 0.6f)
        BoxWithConstraints(Modifier.fillMaxSize().imePadding()) {
            Column(
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .heightIn(min = maxHeight)
                    .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)),
                verticalArrangement = HeroAboveSheet,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Hero()
                AuthSheet(state, onSignIn, onRegister, onGoogle)
            }
        }
    }
}

/** Two children: the last sits at the bottom, the first is centred in the space above it. */
private object HeroAboveSheet : Arrangement.Vertical {
    override fun Density.arrange(totalSize: Int, sizes: IntArray, outPositions: IntArray) {
        val sheetTop = totalSize - sizes.last()
        outPositions[0] = ((sheetTop - sizes.first()) / 2).coerceAtLeast(0)
        outPositions[sizes.lastIndex] = sheetTop
    }
}

@Composable
private fun Hero() {
    val status = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
    Column(
        Modifier.padding(start = 24.dp, end = 24.dp, top = status + 32.dp, bottom = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        LoomMark(Modifier.size(72.dp), animate = !LocalInspectionMode.current)
        Spacer(Modifier.height(20.dp))
        GradientText("Changeloom", Modifier.enter(delayMillis = 350), style = MaterialTheme.typography.displayMedium)
        Spacer(Modifier.height(10.dp))
        WordRiseText(
            "Every release that matters, woven into one feed.",
            Modifier.widthIn(max = 320.dp),
            style = MaterialTheme.typography.titleMedium,
            color = ChangeloomTheme.colors.fgMuted,
            startDelayMillis = 600,
            horizontalArrangement = Arrangement.Center,
        )
    }
}

@Composable
private fun AuthSheet(
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

    // Shake the form whenever a new error arrives (the view model clears it at the start of each attempt).
    val shake = remember { Animatable(0f) }
    val shakeDistance = with(LocalDensity.current) { 10.dp.toPx() }
    LaunchedEffect(state.error) {
        if (state.error != null) {
            for (step in listOf(-1f, 1f, -0.6f, 0.6f, -0.3f, 0f)) shake.animateTo(step * shakeDistance, tween(55))
        }
    }

    Column(
        Modifier
            .widthIn(max = SHEET_MAX_WIDTH)
            .fillMaxWidth()
            .enter(delayMillis = 900)
            .shadow(32.dp, Radius.sheet, ambientColor = Color.Black.copy(alpha = 0.25f), spotColor = c.primary.copy(alpha = 0.35f))
            .clip(Radius.sheet)
            .background(c.surface)
            .border(1.dp, c.lineStrong, Radius.sheet)
            .windowInsetsPadding(WindowInsets.navigationBars)
            .padding(start = 20.dp, end = 20.dp, top = 28.dp, bottom = 16.dp),
    ) {
        AnimatedContent(mode, transitionSpec = { fadeIn(expoTween()) togetherWith fadeOut(expoTween()) }, label = "authTitle") { m ->
            Column {
                Text(m.title, style = MaterialTheme.typography.headlineSmall, color = c.fg)
                Spacer(Modifier.height(4.dp))
                Text(m.subtitle, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
            }
        }
        Spacer(Modifier.height(24.dp))
        SecondaryButton(
            text = "Continue with Google",
            onClick = onGoogle,
            modifier = Modifier.fillMaxWidth(),
            enabled = !state.busy,
            leading = { GoogleMark(Modifier.size(18.dp)) },
        )
        Row(Modifier.padding(vertical = 20.dp), verticalAlignment = Alignment.CenterVertically) {
            HorizontalDivider(Modifier.weight(1f), color = c.line)
            Eyebrow("or with email", Modifier.padding(horizontal = 12.dp))
            HorizontalDivider(Modifier.weight(1f), color = c.line)
        }
        Column(Modifier.graphicsLayer { translationX = shake.value }) {
            ChangeloomTextField(
                value = email,
                onValueChange = { email = it },
                label = "Email",
                placeholder = "you@example.com",
                leadingIcon = Icons.Rounded.AlternateEmail,
                enabled = !state.busy,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
            )
            Spacer(Modifier.height(16.dp))
            ChangeloomTextField(
                value = password,
                onValueChange = { password = it },
                label = "Password",
                leadingIcon = Icons.Rounded.Lock,
                enabled = !state.busy,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { if (filled && !state.busy) submit() }),
                visualTransformation = if (showPassword) VisualTransformation.None else PasswordVisualTransformation(),
                supportingText = if (mode == AuthMode.Register) "At least $MIN_PASSWORD_LENGTH characters" else null,
                trailing = {
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
                Modifier.padding(top = 16.dp),
                tone = BannerTone.Error,
            )
            Spacer(Modifier.height(20.dp))
            PrimaryButton(
                text = mode.action,
                onClick = submit,
                modifier = Modifier.fillMaxWidth(),
                enabled = filled,
                loading = state.busy,
                trailingIcon = Icons.AutoMirrored.Rounded.ArrowForward,
            )
        }
        Row(
            Modifier.fillMaxWidth().padding(top = 8.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(mode.switchPrompt, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
            TextAction(mode.other.action, onClick = { mode = mode.other }, enabled = !state.busy)
        }
    }
}

private class Orb(val color: Color, val x: Float, val y: Float, val phase: Float)

/** Large soft colour fields drifting slowly across the top half: an aurora behind the hero. */
@Composable
private fun DriftingOrbs(modifier: Modifier) {
    val c = ChangeloomTheme.colors
    val alpha = if (c.isDark) 0.30f else 0.14f
    val orbs = remember(c) {
        listOf(Orb(c.primary, 0.15f, 0.04f, 0f), Orb(c.accent, 0.92f, 0.20f, 2.1f), Orb(c.blue, 0.35f, 0.42f, 4.2f))
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
