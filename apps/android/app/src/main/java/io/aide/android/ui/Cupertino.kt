package io.aide.android.ui

import android.os.Build
import android.view.WindowManager
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.core.RepeatMode
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
import androidx.compose.foundation.interaction.collectIsFocusedAsState
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
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.Stroke
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

/** The prominent Fluent button: white words on #0078d4 accent (or #e81123 danger), 10dp rounded. */
@Composable
fun FilledButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, destructive: Boolean = false, large: Boolean = false,
    contentPadding: PaddingValues = PaddingValues(horizontal = 16.dp, vertical = 8.dp), content: @Composable RowScope.() -> Unit,
) = IosButton(
    onClick, modifier, enabled, if (!enabled) aide.fill else if (destructive) aide.red else aide.accent,
    if (enabled) Color.White else aide.disabled, RoundedCornerShape(Radius.Md), if (large) 48.dp else 36.dp, contentPadding,
    (if (large) IosType.Headline else IosType.Subheadline).copy(fontWeight = FontWeight.SemiBold), content,
)

/** Standard Fluent secondary button (or red-text subtle button when destructive, e.g. 中断 per #106). */
@Composable
fun TintedButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, destructive: Boolean = false,
    contentPadding: PaddingValues = PaddingValues(horizontal = 14.dp, vertical = 7.dp), content: @Composable RowScope.() -> Unit,
) {
    val bg = when {
        !enabled -> aide.fill
        destructive -> aide.redSubtle
        else -> aide.card
    }
    val fg = when {
        !enabled -> aide.disabled
        destructive -> aide.redText
        else -> MaterialTheme.colorScheme.onSurface
    }
    val shape = RoundedCornerShape(Radius.Md)
    val mod = if (enabled && !destructive) modifier.border(1.dp, aide.controlBorder, shape) else modifier
    IosButton(onClick, mod, enabled, bg, fg, shape, 36.dp, contentPadding, IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold), content)
}

/** The subtle Fluent button: blue text (#005a9e light / #2899f5 dark), dimmed while pressed. */
@Composable
fun PlainButton(
    onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true,
    contentPadding: PaddingValues = PaddingValues(horizontal = 10.dp, vertical = 8.dp), content: @Composable RowScope.() -> Unit,
) = IosButton(onClick, modifier, enabled, Color.Transparent, if (enabled) MaterialTheme.colorScheme.primary else aide.disabled, RoundedCornerShape(Radius.Md), 36.dp, contentPadding, IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold), content)

/** A bar's icon button: 48dp touch target, dimmed while pressed. */
@Composable
fun BarButton(onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, content: @Composable () -> Unit) {
    Box(modifier.size(48.dp).dimPress(enabled, onClick = onClick), contentAlignment = Alignment.Center) {
        CompositionLocalProvider(LocalContentColor provides if (enabled) MaterialTheme.colorScheme.primary else aide.disabled) { content() }
    }
}

/**
 * Fluent navigation bar (工单 #106): 56dp height, acrylic surface over 1px divider,
 * back chevron on the left, title in 16sp SemiBold, actions on the right.
 */
@Composable
fun NavBar(back: (() -> Unit)?, actions: @Composable RowScope.() -> Unit = {}, title: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().background(aide.bar).windowInsetsPadding(WindowInsets.statusBars)) {
        Layout(
            content = {
                Box {
                    if (back != null) Row(
                        Modifier.heightIn(min = 48.dp).semantics(mergeDescendants = true) { contentDescription = t("Back", "返回") }.dimPress(onClick = back).padding(end = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(BackChevron, null, Modifier.size(22.dp), tint = MaterialTheme.colorScheme.primary)
                        Text(t("Back", "返回"), style = IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold), color = MaterialTheme.colorScheme.primary)
                    }
                }
                Row(verticalAlignment = Alignment.CenterVertically, content = actions)
                Box(contentAlignment = Alignment.Center) {
                    CompositionLocalProvider(LocalTextStyle provides IosType.Headline.copy(textAlign = TextAlign.Center), LocalContentColor provides MaterialTheme.colorScheme.onSurface) { title() }
                }
            },
            modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp).padding(horizontal = 4.dp),
        ) { ms, c ->
            val free = c.copy(minWidth = 0, minHeight = 0)
            val lead = ms[0].measure(free)
            val trail = ms[1].measure(free)
            val side = maxOf(lead.width, trail.width) + 4.dp.roundToPx()
            val title = ms[2].measure(Constraints(maxWidth = (c.maxWidth - 2 * side).coerceAtLeast(0)))
            val h = maxOf(52.dp.roundToPx(), lead.height, trail.height, title.height + 8.dp.roundToPx())
            layout(c.maxWidth, h) {
                lead.place(0, (h - lead.height) / 2)
                trail.place(c.maxWidth - trail.width, (h - trail.height) / 2)
                title.place((c.maxWidth - title.width) / 2, (h - title.height) / 2)
            }
        }
        Hairline()
    }
}

/** Fluent toggle switch (工单 #106): 40×20dp pill track, 12dp circle thumb, outlined off / #0078d4 filled on. */
@Composable
fun IosSwitch(checked: Boolean, onCheckedChange: ((Boolean) -> Unit)?, modifier: Modifier = Modifier, enabled: Boolean = true) {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val pos by animateFloatAsState(if (checked) 1f else 0f, tween(Motion.Short, easing = Motion.EaseOut), label = "switch")
    val stretch by animateDpAsState(if (pressed && enabled) 4.dp else 0.dp, tween(Motion.Short, easing = Motion.EaseOut), label = "knob")
    val trackBg = lerp(Color.Transparent, aide.accent, pos)
    val trackBorder = lerp(aide.controlStrong, aide.accent, pos)
    val knobColor = lerp(aide.controlStrong, Color.White, pos)
    Box(
        modifier.size(40.dp, 20.dp).graphicsLayer { alpha = if (enabled) 1f else 0.45f }.clip(CircleShape)
            .background(trackBg).border(1.dp, trackBorder, CircleShape)
            .then(if (onCheckedChange != null) Modifier.toggleable(value = checked, interactionSource = interaction, indication = null, enabled = enabled, role = Role.Switch, onValueChange = { onCheckedChange(it) }) else Modifier),
    ) {
        val knob = 12.dp + stretch
        Box(
            Modifier.offset { IntOffset(((4.dp + (40.dp - 8.dp - knob) * pos).toPx()).toInt(), 4.dp.roundToPx()) }
                .size(knob, 12.dp).background(knobColor, CircleShape),
        )
    }
}

/** A Fluent checkbox/radio tick: 20dp rounded square or circle, #0078d4 filled with white tick when checked. */
@Composable
fun RoundCheck(on: Boolean) {
    Box(
        Modifier.size(20.dp).clip(CircleShape).then(if (on) Modifier.background(aide.accent) else Modifier.border(1.5.dp, aide.controlStrong, CircleShape)),
        contentAlignment = Alignment.Center,
    ) { if (on) Icon(CheckMark, null, Modifier.size(13.dp), tint = Color.White) }
}

/** One choice of a list: 48dp touch row, 3dp blue left indicator when picked, checkmark on the right. */
@Composable
fun CheckRow(text: String, checked: Boolean, sub: String? = null, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 48.dp)
            .then(if (checked) Modifier.background(aide.accentSubtle) else Modifier)
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

/** Rows as one Fluent grouped card, a hairline between each two from [inset]. */
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

/** Fluent pill segmented control (工单 #106: pill outer track & inner selected pill). */
@Composable
fun <T> Segmented(options: List<Pair<T, String>>, selected: T, modifier: Modifier = Modifier, onSelect: (T) -> Unit) {
    val i = options.indexOfFirst { it.first == selected }.coerceAtLeast(0)
    val at by animateFloatAsState(i.toFloat(), tween(Motion.Short, easing = Motion.EaseOut), label = "segment")
    BoxWithConstraints(modifier.height(36.dp).clip(CircleShape).background(aide.fill).padding(3.dp)) {
        val w = maxWidth / options.size
        Box(
            Modifier.offset { IntOffset((w * at).roundToPx(), 0) }.width(w).fillMaxHeight()
                .shadow(2.dp, CircleShape, ambientColor = Color.Black.copy(alpha = 0.12f), spotColor = Color.Black.copy(alpha = 0.12f))
                .background(aide.segment, CircleShape),
        )
        Row(Modifier.fillMaxWidth().fillMaxHeight()) {
            options.forEach { (v, label) ->
                val on = v == selected
                Box(
                    Modifier.weight(1f).fillMaxHeight().selectable(on, remember { MutableInteractionSource() }, null, role = Role.Tab) { onSelect(v) },
                    contentAlignment = Alignment.Center,
                ) { Text(label, style = IosType.Subheadline, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
        }
    }
}

/** Fluent stepper: − and + on a 10dp rounded bordered track, a divider between. */
@Composable
fun IosStepper(minusEnabled: Boolean, plusEnabled: Boolean, onMinus: () -> Unit, onPlus: () -> Unit) {
    val shape = RoundedCornerShape(Radius.Md)
    Row(Modifier.height(34.dp).clip(shape).background(aide.card).border(1.dp, aide.controlBorder, shape), verticalAlignment = Alignment.CenterVertically) {
        @Composable fun Half(text: String, enabled: Boolean, onClick: () -> Unit) = Box(
            Modifier.width(44.dp).fillMaxHeight().clickable(remember { MutableInteractionSource() }, Highlight(aide.pressed), enabled = enabled, role = Role.Button, onClick = onClick),
            contentAlignment = Alignment.Center,
        ) { Text(text, fontSize = 18.sp, color = if (enabled) MaterialTheme.colorScheme.onSurface else aide.disabled) }
        Half("−", minusEnabled, onMinus)
        Box(Modifier.width(1.dp).height(18.dp).background(aide.separator))
        Half("+", plusEnabled, onPlus)
    }
}

/** Fluent text field (工单 #106): 10dp rounded, 1dp control border + animated bottom accent line (1dp → 2dp Fluent blue on focus), 44dp min height. */
@Composable
fun IosField(
    value: String, onValueChange: (String) -> Unit, modifier: Modifier = Modifier, label: String? = null, placeholder: String? = null,
    enabled: Boolean = true, singleLine: Boolean = true, password: Boolean = false, visualTransformation: VisualTransformation? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default, supportingText: (@Composable () -> Unit)? = null,
) {
    val fieldShape = RoundedCornerShape(Radius.Md)
    val dark = isSystemInDarkTheme()
    val fieldBg = if (!enabled) aide.fill else if (dark) Color(0x0AFFFFFF) else Color.White
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    val accentLineColor by animateColorAsState(
        targetValue = when {
            !enabled -> Color.Transparent
            focused -> aide.accent
            else -> aide.controlAccentLine
        },
        animationSpec = tween(Motion.Short, easing = Motion.EaseOut),
        label = "fieldAccent",
    )
    val accentLineWidth by animateDpAsState(if (focused && enabled) 2.dp else 1.dp, tween(Motion.Short, easing = Motion.EaseOut), label = "fieldLineW")
    Column(modifier, verticalArrangement = Arrangement.spacedBy(5.dp)) {
        label?.let { Text(it, style = IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold), color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.padding(start = 2.dp)) }
        BasicTextField(
            value, onValueChange, Modifier.fillMaxWidth().semantics { label?.let { contentDescription = it } }, enabled = enabled, singleLine = singleLine,
            textStyle = IosType.Subheadline.copy(color = if (enabled) MaterialTheme.colorScheme.onSurface else aide.disabled),
            visualTransformation = visualTransformation ?: if (password) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = keyboardOptions, interactionSource = interaction, cursorBrush = SolidColor(aide.accent),
            decorationBox = { inner ->
                Box(
                    Modifier.fillMaxWidth().heightIn(min = 44.dp).clip(fieldShape).background(fieldBg)
                        .border(1.dp, if (enabled) aide.controlBorder else Color.Transparent, fieldShape)
                        .drawBehind {
                            if (enabled && accentLineColor != Color.Transparent) {
                                val h = accentLineWidth.toPx()
                                drawRect(accentLineColor, topLeft = Offset(0f, size.height - h), size = Size(size.width, h))
                            }
                        }
                        .padding(horizontal = 12.dp, vertical = 11.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    if (value.isEmpty() && placeholder != null) Text(placeholder, style = IosType.Subheadline, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = if (singleLine) 1 else 3, overflow = TextOverflow.Ellipsis)
                    inner()
                }
            },
        )
        supportingText?.let { CompositionLocalProvider(LocalTextStyle provides IosType.Caption, LocalContentColor provides MaterialTheme.colorScheme.onSurfaceVariant) { Box(Modifier.padding(start = 2.dp)) { it() } } }
    }
}

/** Fluent 2 indeterminate ProgressRing: smooth continuous rotation + breathing arc sweep on Canvas without recomposing. */
@Composable
fun ActivityIndicator(modifier: Modifier = Modifier, size: Dp = 20.dp, color: Color = aide.accent) {
    val transition = rememberInfiniteTransition(label = "ring")
    val angle by transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(1100, easing = LinearEasing)),
        label = "angle",
    )
    val sweep by transition.animateFloat(
        initialValue = 50f,
        targetValue = 220f,
        animationSpec = infiniteRepeatable(tween(900, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "sweep",
    )
    val track = color.copy(alpha = 0.16f)
    Canvas(modifier.size(size)) {
        val strokeWidth = (this.size.minDimension * 0.11f).coerceIn(2.dp.toPx(), 3.5.dp.toPx())
        val inset = strokeWidth / 2f
        val arcSize = Size(this.size.width - strokeWidth, this.size.height - strokeWidth)
        val topLeft = Offset(inset, inset)
        val stroke = Stroke(width = strokeWidth, cap = StrokeCap.Round)
        drawArc(color = track, startAngle = 0f, sweepAngle = 360f, useCenter = false, topLeft = topLeft, size = arcSize, style = stroke)
        drawArc(color = color, startAngle = angle - 90f, sweepAngle = sweep, useCenter = false, topLeft = topLeft, size = arcSize, style = stroke)
    }
}

/** Floating Fluent snackbar banner (16dp radius). */
@Composable
fun IosSnackbarHost(state: SnackbarHostState, modifier: Modifier = Modifier) {
    SnackbarHost(state, modifier) { d ->
        val shape = RoundedCornerShape(Radius.Lg)
        Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), contentAlignment = Alignment.Center) {
            Row(
                Modifier.shadow(16.dp, shape, ambientColor = Color.Black.copy(alpha = 0.2f), spotColor = Color.Black.copy(alpha = 0.2f))
                    .clip(shape).background(aide.menu).border(1.dp, aide.cardEdge, shape).padding(horizontal = 16.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(d.visuals.message, Modifier.weight(1f, fill = false), style = IosType.Subheadline, color = MaterialTheme.colorScheme.onSurface)
                // Its action (Undo, 工单 #165), at its end.
                d.visuals.actionLabel?.let { a ->
                    Spacer(Modifier.width(16.dp))
                    Text(a, Modifier.dimPress { d.performAction() }, style = IosType.Subheadline, fontWeight = FontWeight.SemiBold, color = aide.blueOnSubtle)
                }
            }
        }
    }
}

/** Fluent bottom sheet (工单 #106): 24dp top corners, 32×4dp pill grabber, elevated Fluent surface. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun IosSheet(onDismissRequest: () -> Unit, title: String? = null, content: @Composable ColumnScope.() -> Unit) {
    val k = aide.elevated(isSystemInDarkTheme())
    ModalBottomSheet(
        onDismissRequest = onDismissRequest, containerColor = k.grouped, contentColor = MaterialTheme.colorScheme.onSurface,
        shape = RoundedCornerShape(topStart = Radius.Xl, topEnd = Radius.Xl), tonalElevation = 0.dp, scrimColor = Color.Black.copy(alpha = 0.45f),
        dragHandle = { Box(Modifier.padding(top = 8.dp, bottom = 6.dp).size(32.dp, 4.dp).clip(CircleShape).background(k.controlAccentLine)) },
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

/** Behind a dialog's window: dimmed (and blurred on Android 12+). */
@Composable
private fun BlurBehind() {
    val view = LocalView.current
    DisposableEffect(view) {
        (view.parent as? DialogWindowProvider)?.window?.let { w ->
            w.setDimAmount(0.45f)
            if (Build.VERSION.SDK_INT >= 31) {
                w.addFlags(WindowManager.LayoutParams.FLAG_BLUR_BEHIND)
                w.attributes = w.attributes.apply { blurBehindRadius = 40 }
            }
        }
        onDispose {}
    }
}

/**
 * Fluent dialog (工单 #106): 24dp rounded surface, 24dp padding, clear heading & body,
 * Fluent filled/destructive/secondary action buttons.
 */
@Composable
fun IosAlert(onDismissRequest: () -> Unit, title: String, actions: List<AlertAction>, message: String? = null, content: (@Composable ColumnScope.() -> Unit)? = null) {
    Dialog(onDismissRequest, DialogProperties(usePlatformDefaultWidth = false)) {
        BlurBehind()
        val k = aide.elevated(isSystemInDarkTheme())
        val shape = RoundedCornerShape(Radius.Xl)
        CompositionLocalProvider(LocalAideTokens provides k) {
            Column(Modifier.widthIn(min = 296.dp, max = 340.dp).clip(shape).background(k.menu).border(1.dp, k.cardEdge, shape).padding(20.dp)) {
                Column(
                    Modifier.fillMaxWidth().heightIn(max = 460.dp).verticalScroll(rememberScrollState()).padding(bottom = 16.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(title, style = IosType.Title3)
                    message?.let { Text(it, style = IosType.Subheadline, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    content?.let { c ->
                        Column(Modifier.fillMaxWidth().padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            CompositionLocalProvider(LocalTextStyle provides IosType.Subheadline) { c() }
                        }
                    }
                }
                @Composable fun Btn(a: AlertAction, m: Modifier) {
                    when (a.role) {
                        AlertRole.Preferred -> FilledButton(a.onClick, m, enabled = a.enabled) { Text(a.text) }
                        AlertRole.Destructive -> FilledButton(a.onClick, m, enabled = a.enabled, destructive = true) { Text(a.text) }
                        AlertRole.Cancel, AlertRole.Default -> TintedButton(a.onClick, m, enabled = a.enabled) { Text(a.text) }
                    }
                }
                if (actions.size == 2) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Btn(actions[0], Modifier.weight(1f))
                        Btn(actions[1], Modifier.weight(1f))
                    }
                } else Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    actions.forEach { a -> Btn(a, Modifier.fillMaxWidth()) }
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
 * Fluent Flyout menu (工单 #106): 16dp rounded panel, 8dp inner padding, 48dp touch rows,
 * 10dp rounded item highlight with 3dp #0078d4 left indicator on active/checked item.
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
            enter = scaleIn(tween(Motion.Medium, easing = Motion.EaseOut), initialScale = 0.94f, transformOrigin = TransformOrigin(0.15f, 0f)) + fadeIn(tween(Motion.Short)),
            exit = scaleOut(tween(Motion.Short), targetScale = 0.96f, transformOrigin = TransformOrigin(0.15f, 0f)) + fadeOut(tween(Motion.Short)),
        ) {
            val shape = RoundedCornerShape(Radius.Lg)
            val rowShape = RoundedCornerShape(Radius.Md)
            val ticks = entries.any { it.checked }
            Column(
                Modifier.padding(12.dp).widthIn(min = 200.dp, max = 290.dp)
                    .shadow(20.dp, shape, ambientColor = Color.Black.copy(alpha = 0.24f), spotColor = Color.Black.copy(alpha = 0.24f))
                    .clip(shape).background(aide.menu).border(1.dp, aide.cardEdge, shape).padding(6.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                entries.forEach { e ->
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 48.dp).clip(rowShape)
                            .then(if (e.checked) Modifier.background(aide.accentSubtle) else Modifier)
                            .clickable(remember { MutableInteractionSource() }, Highlight(aide.pressed), enabled = e.enabled, role = Role.Button) { e.onClick() }
                            .padding(start = 6.dp, end = 12.dp, top = 10.dp, bottom = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(Modifier.width(3.dp).height(18.dp).clip(CircleShape).background(if (e.checked) aide.accent else Color.Transparent))
                        Spacer(Modifier.width(9.dp))
                        if (ticks) Box(Modifier.width(22.dp)) { if (e.checked) Icon(CheckMark, null, Modifier.size(15.dp), tint = MaterialTheme.colorScheme.primary) }
                        Text(e.text, style = IosType.Subheadline, color = when {
                            !e.enabled -> aide.disabled
                            e.destructive -> aide.redText
                            else -> MaterialTheme.colorScheme.onSurface
                        })
                    }
                }
            }
        }
    }
}
