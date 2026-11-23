package io.aide.android.ui

import android.os.Build
import android.view.WindowManager
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupPositionProvider
import androidx.compose.ui.window.PopupProperties
import kotlin.math.floor

// Apple's controls, drawn here with Compose (工单 #95): no Material control shows its own look anywhere in the app — the
// switch, buttons, alerts, sheets, menus, fields, segmented control, stepper, navigation and tab bars, the spinner are
// these. Each follows the HIG component of the same name, at iOS's sizes.

/** A hairline: one physical pixel, as iOS's separators. */
@Composable
fun Hairline(modifier: Modifier = Modifier, color: Color = aide.separator) {
    val px = with(LocalDensity.current) { 1f.toDp() }
    Box(modifier.fillMaxWidth().height(px).background(color))
}

private fun chevron(name: String, from: Offset, mid: Offset, to: Offset, width: Float) = ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f).path(
    stroke = SolidColor(Color.Black), strokeLineWidth = width, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round,
) { moveTo(from.x, from.y); lineTo(mid.x, mid.y); lineTo(to.x, to.y) }.build()

/** chevron.backward, chevron.forward and checkmark, as SF Symbols draw them (thin, round-capped). */
val BackChevron: ImageVector = chevron("Back", Offset(15.5f, 3.5f), Offset(7f, 12f), Offset(15.5f, 20.5f), 2.6f)
val ForwardChevron: ImageVector = chevron("Forward", Offset(9f, 5f), Offset(16f, 12f), Offset(9f, 19f), 2.4f)
val CheckMark: ImageVector = chevron("Check", Offset(4.5f, 12.5f), Offset(9.5f, 17.5f), Offset(19.5f, 6.5f), 2.6f)

/** A row's disclosure chevron (tertiary label). */
@Composable
fun Chevron() = Icon(ForwardChevron, null, Modifier.size(15.dp), tint = aide.faint)

// Buttons (HIG Buttons): prominent (filled with the accent), tinted (the accent at a fifth), plain (the accent's words).
// Pressed, each gives a little and is highlighted; disabled, each is greyed.

@Composable
private fun IosButton(
    onClick: () -> Unit, modifier: Modifier, enabled: Boolean, bg: Color, fg: Color, shape: RoundedCornerShape, minHeight: Dp,
    contentPadding: PaddingValues, style: androidx.compose.ui.text.TextStyle, content: @Composable RowScope.() -> Unit,
) {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.97f else 1f, Motion.snappy(), label = "press")
    Row(
        modifier.graphicsLayer { scaleX = scale; scaleY = scale }.heightIn(min = minHeight).clip(shape).background(bg)
            .clickable(interaction, Highlight(if (bg == Color.Transparent) Color.Transparent else aide.pressed), enabled = enabled, role = Role.Button, onClick = onClick)
            .graphicsLayer { alpha = if (pressed && bg == Color.Transparent) 0.3f else 1f }
            .padding(contentPadding),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center,
    ) { CompositionLocalProvider(LocalContentColor provides fg, LocalTextStyle provides style) { content() } }
}

/** The prominent button (.borderedProminent): white words on the accent (or red); large, the screen's one action. */
@Composable
fun FilledButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, destructive: Boolean = false, large: Boolean = false,
    contentPadding: PaddingValues = PaddingValues(horizontal = 16.dp, vertical = 7.dp), content: @Composable RowScope.() -> Unit,
) = IosButton(
    onClick, modifier, enabled, if (!enabled) aide.fill2 else if (destructive) DangerFill else AccentFill,
    if (enabled) Color.White else aide.faint, RoundedCornerShape(if (large) 12.dp else Radius.Md), if (large) 50.dp else 34.dp, contentPadding,
    (if (large) IosType.Headline else IosType.Subheadline).copy(fontWeight = FontWeight.SemiBold), content,
)

/** The tinted button (.bordered): the accent's words on the accent's tint. */
@Composable
fun TintedButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, destructive: Boolean = false,
    contentPadding: PaddingValues = PaddingValues(horizontal = 14.dp, vertical = 7.dp), content: @Composable RowScope.() -> Unit,
) {
    val c = if (destructive) aide.red else MaterialTheme.colorScheme.primary
    IosButton(onClick, modifier, enabled, if (enabled) c.copy(alpha = 0.15f) else aide.fill2, if (enabled) (if (destructive) aide.redText else aide.blueText) else aide.faint, RoundedCornerShape(Radius.Md), 34.dp, contentPadding, IosType.Subheadline.copy(fontWeight = FontWeight.Medium), content)
}

/** The plain button (.borderless): the accent's words, dimmed while pressed. */
@Composable
fun PlainButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true,
    contentPadding: PaddingValues = PaddingValues(horizontal = 10.dp, vertical = 8.dp), content: @Composable RowScope.() -> Unit,
) = IosButton(onClick, modifier, enabled, Color.Transparent, if (enabled) MaterialTheme.colorScheme.primary else aide.faint, RoundedCornerShape(Radius.Md), 36.dp, contentPadding, IosType.Subheadline, content)

/** A bar's icon button: the accent, 44 square, dimmed while pressed. */
@Composable
fun BarButton(onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, content: @Composable () -> Unit) {
    Box(modifier.size(44.dp).dimPress(enabled, onClick = onClick), contentAlignment = Alignment.Center) {
        CompositionLocalProvider(LocalContentColor provides if (enabled) MaterialTheme.colorScheme.primary else aide.faint) { content() }
    }
}

/**
 * iOS's navigation bar (HIG Navigation bars): the back chevron with "Back" on the left, the page's title centred in
 * Headline, its buttons on the right, a hairline under it, on the bars' material.
 */
@Composable
fun NavBar(back: (() -> Unit)?, actions: @Composable RowScope.() -> Unit = {}, title: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().background(aide.bar).windowInsetsPadding(WindowInsets.statusBars)) {
        Layout(
            content = {
                Box {
                    if (back != null) Row(
                        Modifier.heightIn(min = 44.dp).semantics(mergeDescendants = true) { contentDescription = t("Back", "返回") }.dimPress(onClick = back).padding(end = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(BackChevron, null, Modifier.size(24.dp), tint = MaterialTheme.colorScheme.primary)
                        Text(t("Back", "返回"), style = IosType.Body, color = MaterialTheme.colorScheme.primary)
                    }
                }
                Row(verticalAlignment = Alignment.CenterVertically, content = actions)
                Box(contentAlignment = Alignment.Center) {
                    CompositionLocalProvider(LocalTextStyle provides IosType.Headline.copy(textAlign = TextAlign.Center), LocalContentColor provides MaterialTheme.colorScheme.onSurface) { title() }
                }
            },
            modifier = Modifier.fillMaxWidth().heightIn(min = 44.dp).padding(horizontal = 4.dp),
        ) { ms, c ->
            val free = c.copy(minWidth = 0, minHeight = 0)
            val lead = ms[0].measure(free)
            val trail = ms[1].measure(free)
            val side = maxOf(lead.width, trail.width) + 4.dp.roundToPx()
            val title = ms[2].measure(Constraints(maxWidth = (c.maxWidth - 2 * side).coerceAtLeast(0)))
            val h = maxOf(44.dp.roundToPx(), lead.height, trail.height, title.height + 6.dp.roundToPx())
            layout(c.maxWidth, h) {
                lead.place(0, (h - lead.height) / 2)
                trail.place(c.maxWidth - trail.width, (h - trail.height) / 2)
                title.place((c.maxWidth - title.width) / 2, (h - title.height) / 2)
            }
        }
        Hairline()
    }
}

/** iOS's switch (HIG Toggles): 51×31, green while on, a white knob with a shadow that stretches under the finger. */
@Composable
fun IosSwitch(checked: Boolean, onCheckedChange: ((Boolean) -> Unit)?, modifier: Modifier = Modifier, enabled: Boolean = true) {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val pos by animateFloatAsState(if (checked) 1f else 0f, Motion.snappy(), label = "switch")
    val stretch by animateDpAsState(if (pressed && enabled) 7.dp else 0.dp, Motion.snappy(), label = "knob")
    val track = lerp(aide.switchOff, aide.green, pos)
    Box(
        modifier.size(51.dp, 31.dp).graphicsLayer { alpha = if (enabled) 1f else 0.5f }.clip(CircleShape).background(track)
            .then(if (onCheckedChange != null) Modifier.toggleable(value = checked, interactionSource = interaction, indication = null, enabled = enabled, role = Role.Switch, onValueChange = { onCheckedChange(it) }) else Modifier),
    ) {
        val knob = 27.dp + stretch
        Box(
            Modifier.offset { IntOffset(((2.dp + (51.dp - 4.dp - knob) * pos).toPx()).toInt(), 2.dp.roundToPx()) }
                .size(knob, 27.dp).shadow(3.dp, CircleShape, ambientColor = Color.Black.copy(alpha = 0.2f), spotColor = Color.Black.copy(alpha = 0.25f)).background(Color.White, CircleShape),
        )
    }
}

/** A round tick (as iOS's selection circles): an empty ring, or the accent filled with a white tick. */
@Composable
fun RoundCheck(on: Boolean) {
    Box(
        Modifier.size(22.dp).clip(CircleShape).then(if (on) Modifier.background(MaterialTheme.colorScheme.primary) else Modifier.border(1.5.dp, aide.faint, CircleShape)),
        contentAlignment = Alignment.Center,
    ) { if (on) Icon(CheckMark, null, Modifier.size(14.dp), tint = Color.White) }
}

/** One choice of a list (an inline picker, as iOS's): the words, a tick on the right while picked; a row of an inset card. */
@Composable
fun CheckRow(text: String, checked: Boolean, sub: String? = null, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 44.dp)
            .selectable(checked, remember { MutableInteractionSource() }, Highlight(aide.pressed), role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(text, style = IosType.Body)
            sub?.takeIf { it.isNotEmpty() }?.let { Text(it, style = IosType.Footnote, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        if (checked) Icon(CheckMark, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary)
    }
}

/** Rows as one inset grouped card, a hairline between each two from [inset]. */
@Composable
fun <T> GroupRows(items: List<T>, modifier: Modifier = Modifier, inset: Dp = 16.dp, row: @Composable (T) -> Unit) {
    if (items.isEmpty()) return
    Column(modifier.fillMaxWidth().aideCard()) {
        items.forEachIndexed { i, x ->
            if (i > 0) Hairline(Modifier.padding(start = inset))
            row(x)
        }
    }
}

/** iOS's segmented control (HIG Segmented controls): a grey track 32 high, the picked segment a raised capsule sliding to it. */
@Composable
fun <T> Segmented(options: List<Pair<T, String>>, selected: T, modifier: Modifier = Modifier, onSelect: (T) -> Unit) {
    val i = options.indexOfFirst { it.first == selected }.coerceAtLeast(0)
    val at by animateFloatAsState(i.toFloat(), Motion.snappy(), label = "segment")
    BoxWithConstraints(modifier.height(32.dp).clip(RoundedCornerShape(9.dp)).background(aide.fill2).padding(2.dp)) {
        val w = maxWidth / options.size
        Box(
            Modifier.offset { IntOffset((w * at).roundToPx(), 0) }.width(w).fillMaxHeight()
                .shadow(2.dp, RoundedCornerShape(7.dp), ambientColor = Color.Black.copy(alpha = 0.12f), spotColor = Color.Black.copy(alpha = 0.12f))
                .background(aide.segment, RoundedCornerShape(7.dp)),
        )
        Row(Modifier.fillMaxWidth().fillMaxHeight()) {
            options.forEach { (v, label) ->
                val on = v == selected
                Box(
                    Modifier.weight(1f).fillMaxHeight().selectable(on, remember { MutableInteractionSource() }, null, role = Role.Tab) { onSelect(v) },
                    contentAlignment = Alignment.Center,
                ) { Text(label, style = IosType.Footnote, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
        }
    }
}

/** iOS's stepper (HIG Steppers): − and + on one grey capsule-rounded track, a hairline between. */
@Composable
fun IosStepper(minusEnabled: Boolean, plusEnabled: Boolean, onMinus: () -> Unit, onPlus: () -> Unit) {
    Row(Modifier.height(32.dp).clip(RoundedCornerShape(Radius.Md)).background(aide.fill2), verticalAlignment = Alignment.CenterVertically) {
        @Composable fun Half(text: String, enabled: Boolean, onClick: () -> Unit) = Box(
            Modifier.width(47.dp).fillMaxHeight().clickable(remember { MutableInteractionSource() }, Highlight(aide.pressed), enabled = enabled, role = Role.Button, onClick = onClick),
            contentAlignment = Alignment.Center,
        ) { Text(text, fontSize = 20.sp, color = if (enabled) MaterialTheme.colorScheme.onSurface else aide.faint) }
        Half("−", minusEnabled, onMinus)
        Box(Modifier.width(1.dp).height(18.dp).background(aide.separator))
        Half("+", plusEnabled, onPlus)
    }
}

/** A text field as iOS draws one: a rounded grey well 44 high, its label (if any) small above it, a note under it. */
@Composable
fun IosField(
    value: String, onValueChange: (String) -> Unit, modifier: Modifier = Modifier, label: String? = null, placeholder: String? = null,
    enabled: Boolean = true, singleLine: Boolean = true, password: Boolean = false, visualTransformation: VisualTransformation? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default, supportingText: (@Composable () -> Unit)? = null,
) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(5.dp)) {
        label?.let { Text(it, style = IosType.Footnote, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 4.dp)) }
        BasicTextField(
            value, onValueChange, Modifier.fillMaxWidth().semantics { label?.let { contentDescription = it } }, enabled = enabled, singleLine = singleLine,
            textStyle = IosType.Body.copy(color = if (enabled) MaterialTheme.colorScheme.onSurface else aide.faint),
            visualTransformation = visualTransformation ?: if (password) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = keyboardOptions, cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            decorationBox = { inner ->
                Box(Modifier.fillMaxWidth().heightIn(min = 44.dp).clip(RoundedCornerShape(Radius.Lg)).background(aide.fill2).padding(horizontal = 12.dp, vertical = 11.dp), contentAlignment = Alignment.CenterStart) {
                    if (value.isEmpty() && placeholder != null) Text(placeholder, style = IosType.Body, color = aide.faint, maxLines = if (singleLine) 1 else 3, overflow = TextOverflow.Ellipsis)
                    inner()
                }
            },
        )
        supportingText?.let { CompositionLocalProvider(LocalTextStyle provides IosType.Footnote, LocalContentColor provides MaterialTheme.colorScheme.onSurfaceVariant) { Box(Modifier.padding(start = 4.dp)) { it() } } }
    }
}

/** iOS's activity indicator (HIG Progress indicators): eight spokes, the bright one going round. Drawn, not recomposed. */
@Composable
fun ActivityIndicator(modifier: Modifier = Modifier, size: Dp = 20.dp, color: Color = MaterialTheme.colorScheme.onSurfaceVariant) {
    val step by rememberInfiniteTransition(label = "spin").animateFloat(0f, 8f, infiniteRepeatable(tween(800, easing = LinearEasing)), label = "step")
    Canvas(modifier.size(size)) {
        val s = floor(step).toInt() % 8
        val r = this.size.minDimension / 2
        for (i in 0 until 8) {
            val a = Math.toRadians(i * 45.0 - 90)
            val fade = ((s - i + 8) % 8) / 8f
            val dir = Offset(kotlin.math.cos(a).toFloat(), kotlin.math.sin(a).toFloat())
            drawLine(color.copy(alpha = color.alpha * (1f - fade * 0.8f)), center + dir * (r * 0.42f), center + dir * (r * 0.92f), r * 0.2f, StrokeCap.Round)
        }
    }
}

/** What an action says, as a floating rounded banner on the menus' material (iOS has no snackbar). */
@Composable
fun IosSnackbarHost(state: SnackbarHostState, modifier: Modifier = Modifier) {
    SnackbarHost(state, modifier) { d ->
        Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), contentAlignment = Alignment.Center) {
            Text(
                d.visuals.message,
                Modifier.shadow(16.dp, RoundedCornerShape(Radius.Xl), ambientColor = Color.Black.copy(alpha = 0.25f), spotColor = Color.Black.copy(alpha = 0.25f))
                    .clip(RoundedCornerShape(Radius.Xl)).background(aide.menu).border(1.dp, aide.cardEdge, RoundedCornerShape(Radius.Xl)).padding(horizontal = 16.dp, vertical = 12.dp),
                style = IosType.Subheadline, color = MaterialTheme.colorScheme.onSurface,
            )
        }
    }
}

/** An iOS sheet (HIG Sheets): risen from the bottom on the grouped background, its top rounded 14, a grabber, the page dimmed. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IosSheet(onDismissRequest: () -> Unit, title: String? = null, content: @Composable ColumnScope.() -> Unit) {
    val k = aide.elevated(isSystemInDarkTheme())
    ModalBottomSheet(
        onDismissRequest = onDismissRequest, containerColor = k.grouped, contentColor = MaterialTheme.colorScheme.onSurface,
        shape = RoundedCornerShape(topStart = Radius.Xl, topEnd = Radius.Xl), tonalElevation = 0.dp, scrimColor = Color.Black.copy(alpha = 0.4f),
        dragHandle = { Box(Modifier.padding(top = 6.dp, bottom = 6.dp).size(36.dp, 5.dp).clip(CircleShape).background(k.faint)) },
    ) {
        CompositionLocalProvider(LocalAideTokens provides k) {
            title?.let { Text(it, style = IosType.Headline, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(bottom = 10.dp)) }
            content()
        }
    }
}

enum class AlertRole { Default, Preferred, Cancel, Destructive }

/** One button of an alert. */
data class AlertAction(val text: String, val role: AlertRole = AlertRole.Default, val enabled: Boolean = true, val onClick: () -> Unit)

/** Behind a dialog's window: the page blurred where the phone can (Android 12+ with cross-window blur on), and dimmed. */
@Composable
private fun BlurBehind() {
    val view = LocalView.current
    DisposableEffect(view) {
        (view.parent as? DialogWindowProvider)?.window?.let { w ->
            w.setDimAmount(0.4f)
            if (Build.VERSION.SDK_INT >= 31) {
                w.addFlags(WindowManager.LayoutParams.FLAG_BLUR_BEHIND)
                w.attributes = w.attributes.apply { blurBehindRadius = 40 }
            }
        }
        onDispose {}
    }
}

/**
 * iOS's alert (HIG Alerts): centred, 270 wide, rounded 14, on the material; the title in Headline and the message in
 * Footnote, centred; the buttons under a hairline, two side by side, more stacked; the preferred one bold, a destructive
 * one red. Fields or choices an alert needs go between the message and the buttons.
 */
@Composable
fun IosAlert(onDismissRequest: () -> Unit, title: String, actions: List<AlertAction>, message: String? = null, content: (@Composable ColumnScope.() -> Unit)? = null) {
    Dialog(onDismissRequest, DialogProperties(usePlatformDefaultWidth = false)) {
        BlurBehind()
        val k = aide.elevated(isSystemInDarkTheme())
        CompositionLocalProvider(LocalAideTokens provides k) {
            Column(Modifier.width(270.dp).clip(RoundedCornerShape(Radius.Xl)).background(k.menu)) {
                Column(
                    Modifier.fillMaxWidth().heightIn(max = 460.dp).verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, top = 19.dp, bottom = 16.dp),
                    horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Text(title, style = IosType.Headline, textAlign = TextAlign.Center)
                    message?.let { Text(it, style = IosType.Footnote, textAlign = TextAlign.Center) }
                    content?.let { c ->
                        Column(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            CompositionLocalProvider(LocalTextStyle provides IosType.Footnote.copy(textAlign = TextAlign.Center)) { c() }
                        }
                    }
                }
                Hairline()
                @Composable fun Button(a: AlertAction, m: Modifier) = Box(
                    m.heightIn(min = 44.dp).clickable(remember { MutableInteractionSource() }, Highlight(k.pressed), enabled = a.enabled, role = Role.Button, onClick = a.onClick).padding(horizontal = 8.dp, vertical = 11.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        a.text, style = IosType.Body, textAlign = TextAlign.Center, fontWeight = if (a.role == AlertRole.Preferred) FontWeight.SemiBold else FontWeight.Normal,
                        color = when {
                            !a.enabled -> k.faint
                            a.role == AlertRole.Destructive -> k.red
                            else -> MaterialTheme.colorScheme.primary
                        },
                    )
                }
                if (actions.size == 2) {
                    Row(Modifier.fillMaxWidth().heightIn(min = 44.dp)) {
                        Button(actions[0], Modifier.weight(1f))
                        Box(Modifier.width(with(LocalDensity.current) { 1f.toDp() }).heightIn(min = 44.dp).background(k.separator))
                        Button(actions[1], Modifier.weight(1f))
                    }
                } else actions.forEachIndexed { i, a ->
                    if (i > 0) Hairline()
                    Button(a, Modifier.fillMaxWidth())
                }
            }
        }
    }
}

/** One item of a menu. */
data class MenuEntry(val text: String, val enabled: Boolean = true, val checked: Boolean = false, val destructive: Boolean = false, val onClick: () -> Unit)

/** Under the anchor, its leading edge on the anchor's (kept on screen), or over it when there is no room under. */
private class UnderAnchor(private val gap: Int, private val margin: Int) : PopupPositionProvider {
    override fun calculatePosition(anchorBounds: IntRect, windowSize: IntSize, layoutDirection: LayoutDirection, popupContentSize: IntSize): IntOffset {
        val x = anchorBounds.left.coerceAtMost(windowSize.width - popupContentSize.width - margin).coerceAtLeast(margin)
        val below = anchorBounds.bottom + gap
        val y = if (below + popupContentSize.height <= windowSize.height - margin) below else (anchorBounds.top - gap - popupContentSize.height).coerceAtLeast(margin)
        return IntOffset(x, y)
    }
}

/**
 * iOS's menu (HIG Menus): a rounded 14 panel on the menus' material with a soft wide shadow, its items 44 high in Body
 * with hairlines between, a tick before the picked one, a destructive one red; it springs open from its button.
 */
@Composable
fun IosMenu(expanded: Boolean, onDismissRequest: () -> Unit, entries: List<MenuEntry>) {
    val state = remember { MutableTransitionState(false) }
    state.targetState = expanded
    if (!state.currentState && !state.targetState) return
    val density = LocalDensity.current
    val position = remember(density) { with(density) { UnderAnchor(6.dp.roundToPx(), 8.dp.roundToPx()) } }
    Popup(popupPositionProvider = position, onDismissRequest = onDismissRequest, properties = PopupProperties(focusable = true)) {
        AnimatedVisibility(
            state,
            enter = scaleIn(Motion.snappy(), initialScale = 0.85f, transformOrigin = TransformOrigin(0.15f, 0f)) + fadeIn(tween(Motion.Short)),
            exit = scaleOut(tween(Motion.Short), targetScale = 0.9f, transformOrigin = TransformOrigin(0.15f, 0f)) + fadeOut(tween(Motion.Short)),
        ) {
            val shape = RoundedCornerShape(Radius.Xl)
            val ticks = entries.any { it.checked }
            Column(
                Modifier.padding(16.dp).widthIn(min = 200.dp, max = 290.dp)
                    .shadow(24.dp, shape, ambientColor = Color.Black.copy(alpha = 0.3f), spotColor = Color.Black.copy(alpha = 0.3f))
                    .clip(shape).background(aide.menu).border(1.dp, aide.cardEdge, shape),
            ) {
                entries.forEachIndexed { i, e ->
                    if (i > 0) Hairline()
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 44.dp)
                            .clickable(remember { MutableInteractionSource() }, Highlight(aide.pressed), enabled = e.enabled, role = Role.Button) { e.onClick() }
                            .padding(start = if (ticks) 10.dp else 16.dp, end = 16.dp, top = 10.dp, bottom = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        if (ticks) Box(Modifier.width(26.dp)) { if (e.checked) Icon(CheckMark, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurface) }
                        Text(e.text, style = IosType.Body, color = when {
                            !e.enabled -> aide.faint
                            e.destructive -> aide.red
                            else -> MaterialTheme.colorScheme.onSurface
                        })
                    }
                }
            }
        }
    }
}
