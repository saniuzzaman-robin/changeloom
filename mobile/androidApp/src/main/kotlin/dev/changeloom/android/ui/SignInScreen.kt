package dev.changeloom.android.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
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
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.imeAnimationTarget
import androidx.compose.foundation.layout.exclude
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsBottomHeight
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.R
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
private const val KEYBOARD_LIFT_MS = 250
private const val ORB_PERIOD_MS = 24_000
private val SHEET_MAX_WIDTH = 520.dp
private val SHEET_BORDER = 1.dp

/** Height (inside the system bars) from which the large hero and the sheet's subtitle still fit without scrolling. */
private val ROOMY_HEIGHT = 720.dp

private val AuthMode.action: Int get() = if (this == AuthMode.SignIn) R.string.sign_in else R.string.create_account
private val AuthMode.title: Int get() = if (this == AuthMode.SignIn) R.string.sign_in_title else R.string.register_title
private val AuthMode.subtitle: Int get() = if (this == AuthMode.SignIn) R.string.sign_in_subtitle else R.string.register_subtitle
private val AuthMode.switchPrompt: Int get() = if (this == AuthMode.SignIn) R.string.sign_in_switch_prompt else R.string.register_switch_prompt

/** What the email form shows and does; the text lives in the view model so it survives process death. */
private class AuthForm(
    val mode: AuthMode,
    val email: String,
    val password: String,
    val onMode: (AuthMode) -> Unit,
    val onEmail: (String) -> Unit,
    val onPassword: (String) -> Unit,
    val onSubmit: () -> Unit,
)

@Composable
fun SignInScreen(vm: SignInViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val mode by vm.mode.collectAsStateWithLifecycle()
    val email by vm.email.collectAsStateWithLifecycle()
    val password by vm.password.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // A second credential request while the account picker is up makes the system cancel the first one
    // ("Cancelled by Changeloom"), so the form stays busy until the picker returns.
    var picking by remember { mutableStateOf(false) }
    SignInContent(
        state = if (picking) state.copy(busy = true) else state,
        form = AuthForm(mode, email, password, vm::setMode, vm::setEmail, vm::setPassword, vm::submit),
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
                        vm.googleFailed(e)
                    } finally {
                        picking = false
                    }
                }
            }
        },
    )
}

/** Brand hero centred in the space above a bottom-anchored form sheet; scrolls once the keyboard leaves too little room. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SignInContent(
    state: SignInState,
    form: AuthForm,
    onGoogle: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    Box(Modifier.fillMaxSize().background(c.bg)) {
        DriftingOrbs(Modifier.matchParentSize())
        GridBackground(Modifier.fillMaxWidth().fillMaxHeight(0.6f), fadeCenter = Offset(0.5f, 0.3f), fadeRadius = 0.6f)
        // The sheet's colour continues behind the navigation bar, so the sheet reads as running to the screen edge.
        Spacer(
            Modifier
                .align(Alignment.BottomCenter)
                .widthIn(max = SHEET_MAX_WIDTH)
                .fillMaxWidth()
                .windowInsetsBottomHeight(WindowInsets.navigationBars)
                .background(c.surface),
        )
        // App rule: content stays between the status bar and the navigation bar (and above the keyboard).
        // The layout ignores the keyboard, so opening it never re-measures the screen or swaps the hero (which
        // would restart its entrance animations). Instead only the sheet is lifted in the draw phase by the
        // keyboard's height, which stays smooth while the keyboard animates; the hero stays put and fades out
        // behind it, and whatever slides off the top is clipped.
        val density = LocalDensity.current
        val navBottom = WindowInsets.navigationBars
        // The lift animates towards the keyboard's final height rather than following its per-frame inset, which
        // some devices report with a bounce while it hides; this way it only ever moves straight to the target.
        val imeTarget = WindowInsets.imeAnimationTarget
        val liftTarget = (imeTarget.getBottom(density) - navBottom.getBottom(density)).coerceAtLeast(0).toFloat()
        val lift by animateFloatAsState(
            targetValue = liftTarget,
            animationSpec = tween(KEYBOARD_LIFT_MS, easing = FastOutSlowInEasing),
            label = "keyboardLift",
        )
        val keyboardLift = { lift }
        // The hero just cross-fades out while the keyboard is up (and back in), on the same clock as the lift.
        val heroAlpha by animateFloatAsState(
            targetValue = if (liftTarget > 0f) 0f else 1f,
            animationSpec = tween(KEYBOARD_LIFT_MS, easing = FastOutSlowInEasing),
            label = "heroAlpha",
        )
        // The sheet's height with the Google row shown; only measured while it is, so collapsing can't undo itself.
        var expandedHeight by remember { mutableIntStateOf(0) }
        BoxWithConstraints(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing.exclude(WindowInsets.ime)).clipToBounds()) {
            // Everything fits without scrolling: the full hero where there's room, a compact one on shorter
            // screens. Scrolling remains only for very large font scales.
            val roomy = maxHeight >= ROOMY_HEIGHT
            // Where the lifted sheet would slide its title off the top, the Google row folds away instead.
            val collapse = liftTarget > 0f && expandedHeight + liftTarget > constraints.maxHeight
            val sheetEdge = with(density) { SHEET_BORDER.toPx() }
            Column(
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .heightIn(min = maxHeight),
                verticalArrangement = HeroAboveSheet,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Hero(
                    roomy,
                    Modifier.graphicsLayer { alpha = heroAlpha },
                )
                AuthSheet(
                    state, form, onGoogle, showSubtitle = roomy, collapseSocial = collapse,
                    modifier = Modifier
                        .onSizeChanged { if (!collapse) expandedHeight = it.height }
                        // The lift can run ahead of the keyboard; the sheet's colour fills the gap below it (tucked
                        // under its bottom edge) so the backdrop never shows between the sheet and the keyboard.
                        .drawBehind {
                            val l = keyboardLift()
                            if (l > 0f) {
                                drawRect(c.surface, Offset(0f, size.height - l - sheetEdge), Size(size.width, l + sheetEdge))
                            }
                        }
                        .graphicsLayer { translationY = -keyboardLift() },
                )
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

/** The mark, name and tagline: stacked and large when [roomy], else the mark beside the name in one row. */
@Composable
private fun Hero(roomy: Boolean, modifier: Modifier = Modifier) {
    val animate = !LocalInspectionMode.current
    Column(
        modifier.padding(horizontal = 24.dp, vertical = if (roomy) 28.dp else 16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (roomy) {
            LoomMark(Modifier.size(64.dp), animate = animate)
            Spacer(Modifier.height(16.dp))
            GradientText(stringResource(R.string.app_brand), Modifier.enter(delayMillis = 350), style = MaterialTheme.typography.displaySmall)
        } else {
            Row(verticalAlignment = Alignment.CenterVertically) {
                LoomMark(Modifier.size(40.dp), animate = animate)
                Spacer(Modifier.width(12.dp))
                GradientText(stringResource(R.string.app_brand), Modifier.enter(delayMillis = 350), style = MaterialTheme.typography.headlineMedium)
            }
        }
        Spacer(Modifier.height(8.dp))
        WordRiseText(
            stringResource(R.string.tagline),
            Modifier.widthIn(max = 320.dp),
            style = if (roomy) MaterialTheme.typography.titleMedium else MaterialTheme.typography.bodyMedium,
            color = ChangeloomTheme.colors.fgMuted,
            startDelayMillis = 600,
            horizontalArrangement = Arrangement.Center,
        )
    }
}

@Composable
private fun AuthSheet(
    state: SignInState,
    form: AuthForm,
    onGoogle: () -> Unit,
    showSubtitle: Boolean,
    collapseSocial: Boolean,
    modifier: Modifier = Modifier,
) {
    val c = ChangeloomTheme.colors
    val focus = LocalFocusManager.current
    val mode = form.mode
    var showPassword by rememberSaveable { mutableStateOf(false) }
    val filled = form.email.isNotBlank() && form.password.isNotEmpty()
    val submit = {
        focus.clearFocus()
        form.onSubmit()
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
        modifier
            .widthIn(max = SHEET_MAX_WIDTH)
            .fillMaxWidth()
            .enter(delayMillis = 900)
            .shadow(32.dp, Radius.sheet, ambientColor = Color.Black.copy(alpha = 0.25f), spotColor = c.primary.copy(alpha = 0.35f))
            .clip(Radius.sheet)
            .background(c.surface)
            .border(SHEET_BORDER, c.lineStrong, Radius.sheet)
            .padding(start = 20.dp, end = 20.dp, top = 22.dp, bottom = 8.dp),
    ) {
        AnimatedContent(mode, transitionSpec = { fadeIn(expoTween()) togetherWith fadeOut(expoTween()) }, label = "authTitle") { m ->
            Column {
                Text(stringResource(m.title), style = MaterialTheme.typography.headlineSmall, color = c.fg)
                if (showSubtitle) {
                    Spacer(Modifier.height(2.dp))
                    Text(stringResource(m.subtitle), style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
                }
            }
        }
        Spacer(Modifier.height(18.dp))
        AnimatedVisibility(
            !collapseSocial,
            enter = expandVertically(expoTween()) + fadeIn(expoTween()),
            exit = shrinkVertically(expoTween()) + fadeOut(expoTween()),
        ) {
            Column {
                SecondaryButton(
                    text = stringResource(R.string.continue_with_google),
                    onClick = onGoogle,
                    modifier = Modifier.fillMaxWidth(),
                    enabled = !state.busy,
                    leading = { GoogleMark(Modifier.size(18.dp)) },
                    dense = true,
                )
                Row(Modifier.padding(vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                    HorizontalDivider(Modifier.weight(1f), color = c.line)
                    Eyebrow(stringResource(R.string.or_with_email), Modifier.padding(horizontal = 12.dp))
                    HorizontalDivider(Modifier.weight(1f), color = c.line)
                }
            }
        }
        Column(Modifier.graphicsLayer { translationX = shake.value }) {
            ChangeloomTextField(
                value = form.email,
                onValueChange = form.onEmail,
                // The field names itself (also its accessibility label), instead of a label row above it.
                placeholder = stringResource(R.string.email),
                leadingIcon = Icons.Rounded.AlternateEmail,
                enabled = !state.busy,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
                dense = true,
            )
            Spacer(Modifier.height(10.dp))
            ChangeloomTextField(
                value = form.password,
                onValueChange = form.onPassword,
                placeholder = stringResource(R.string.password),
                leadingIcon = Icons.Rounded.Lock,
                enabled = !state.busy,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { if (filled && !state.busy) submit() }),
                visualTransformation = if (showPassword) VisualTransformation.None else PasswordVisualTransformation(),
                supportingText = if (mode == AuthMode.Register) stringResource(R.string.password_hint, MIN_PASSWORD_LENGTH) else null,
                trailing = {
                    IconButton(onClick = { showPassword = !showPassword }) {
                        Icon(
                            if (showPassword) Icons.Rounded.VisibilityOff else Icons.Rounded.Visibility,
                            contentDescription = stringResource(if (showPassword) R.string.hide_password else R.string.show_password),
                        )
                    }
                },
                dense = true,
            )
            StatusBanner(
                state.error,
                Icons.Rounded.ErrorOutline,
                Modifier.padding(top = 10.dp),
                tone = BannerTone.Error,
            )
            Spacer(Modifier.height(14.dp))
            PrimaryButton(
                text = stringResource(mode.action),
                onClick = submit,
                modifier = Modifier.fillMaxWidth(),
                enabled = filled,
                loading = state.busy,
                trailingIcon = Icons.AutoMirrored.Rounded.ArrowForward,
                dense = true,
            )
        }
        Row(
            Modifier.fillMaxWidth().padding(top = 2.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(stringResource(mode.switchPrompt), style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
            TextAction(stringResource(mode.other.action), onClick = { form.onMode(mode.other) }, enabled = !state.busy)
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
internal fun webClientId(context: android.content.Context): String {
    val id = context.resources.getIdentifier("default_web_client_id", "string", context.packageName)
    check(id != 0) {
        "Google sign-in is not configured: enable the Google provider and add your SHA-1 in Firebase, then re-download google-services.json"
    }
    return context.getString(id)
}

@Preview(name = "Dark", heightDp = 860)
@Composable
private fun SignInDark() = ChangeloomTheme(ThemeMode.Dark) {
    SignInContent(SignInState(), previewForm(AuthMode.SignIn), onGoogle = {})
}

@Preview(name = "Light, error", heightDp = 860)
@Composable
private fun SignInLight() = ChangeloomTheme(ThemeMode.Light) {
    SignInContent(
        SignInState(error = "The supplied auth credential is incorrect."),
        previewForm(AuthMode.Register),
        onGoogle = {},
    )
}

private fun previewForm(mode: AuthMode) = AuthForm(mode, "ada@example.com", "", {}, {}, {}, {})
