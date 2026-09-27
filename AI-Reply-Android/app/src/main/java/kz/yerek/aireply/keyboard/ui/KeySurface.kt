package kz.yerek.aireply.keyboard.ui

import android.os.SystemClock
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.automirrored.filled.KeyboardReturn
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.automirrored.outlined.Backspace
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.KeyboardCapslock
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.VectorPainter
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.changedToDownIgnoreConsumed
import androidx.compose.ui.input.pointer.changedToUpIgnoreConsumed
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.layout.Box as KeyBox
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.LaidOutKey
import kz.yerek.aireply.keyboard.layout.PageLayout
import kz.yerek.aireply.keyboard.layout.ReturnFace
import kz.yerek.aireply.keyboard.layout.ShiftState

/** Everything the key area draws. Nothing in it changes per keystroke except shift. */
@Immutable
data class KeySurfaceState(
    val layout: PageLayout,
    val labels: KeyboardLabels,
    val shiftMode: ShiftState.Mode,
    val returnFace: ReturnFace,
    val returnProminent: Boolean,
    val returnEnabled: Boolean,
    /** Usually the layout's name; briefly the new one after a switch. */
    val spaceCaption: String,
    /** A request is running: the keys dim and type nowhere. */
    val dimmed: Boolean,
    val languages: List<KeyboardLanguage>
)

/**
 * The key area: ONE canvas and ONE touch handler for every key.
 *
 * WHY NOT A COMPOSABLE PER KEY. Separate key views each own their touches,
 * so the gaps between them belong to nobody - taps there were simply lost,
 * which on a narrow Cyrillic row is a lot of taps. Here the page's hit boxes
 * tile the whole area ([PageLayout.keyIndex]), and drawing is a single pass
 * that a key press only invalidates, never recomposes.
 *
 * Accessibility keeps one node per key, laid over the canvas: TalkBack users
 * explore and double-tap keys as usual.
 */
@Composable
fun KeySurface(
    state: KeySurfaceState,
    theme: KeyboardTheme,
    controller: KeySurfaceController,
    onAccessibilityKey: (KeyboardKey) -> Unit,
    modifier: Modifier = Modifier
) {
    SideEffect {
        controller.layout = state.layout
        controller.isShifted = state.shiftMode != ShiftState.Mode.OFF
        controller.isDimmed = state.dimmed
        controller.languages = state.languages
    }

    // Key captions do not follow the font-size setting: at 1.3x a Cyrillic row
    // already clips, and at 2x the keys would be unusable rather than larger.
    val density = LocalDensity.current
    CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale = 1f)) {
        val measurer = rememberTextMeasurer(cacheSize = 96)
        val icons = KeyIcons(
            shift = rememberVectorPainter(Icons.Outlined.ArrowUpward),
            shiftOn = rememberVectorPainter(Icons.Filled.ArrowUpward),
            capsLock = rememberVectorPainter(Icons.Filled.KeyboardCapslock),
            delete = rememberVectorPainter(Icons.AutoMirrored.Outlined.Backspace),
            globe = rememberVectorPainter(Icons.Filled.Language),
            newline = rememberVectorPainter(Icons.AutoMirrored.Filled.KeyboardReturn),
            send = rememberVectorPainter(Icons.AutoMirrored.Filled.Send),
            search = rememberVectorPainter(Icons.Filled.Search),
            go = rememberVectorPainter(Icons.AutoMirrored.Filled.ArrowForward),
            done = rememberVectorPainter(Icons.Filled.Check),
            previous = rememberVectorPainter(Icons.AutoMirrored.Filled.ArrowBack)
        )

        Box(modifier.fillMaxWidth().height(state.layout.height.dp)) {
            Canvas(
                Modifier
                    .matchParentSize()
                    .pointerInput(controller) { handleTouches(controller) }
            ) {
                drawPage(state, theme, controller.visual, measurer, icons)
            }

            // One accessibility node per key, over the canvas. No pointer
            // input: touches still reach the surface above.
            state.layout.keys.forEach { key ->
                Box(
                    Modifier
                        .offset(key.frame.left.dp, key.frame.top.dp)
                        .size(key.frame.width.dp, key.frame.height.dp)
                        .semantics {
                            contentDescription = describe(key, state)
                            role = Role.Button
                            onClick {
                                onAccessibilityKey(key.key)
                                true
                            }
                        }
                )
            }
        }
    }
}

// ------------------------------------------------------------------ input

private suspend fun androidx.compose.ui.input.pointer.PointerInputScope.handleTouches(
    controller: KeySurfaceController
) {
    val dp = density
    awaitPointerEventScope {
        try {
            while (true) {
                val deadline = controller.nextDeadline()
                val event = if (deadline == null) {
                    awaitPointerEvent(PointerEventPass.Main)
                } else {
                    val wait = (deadline - SystemClock.uptimeMillis()).coerceAtLeast(1L)
                    withTimeoutOrNull(wait) { awaitPointerEvent(PointerEventPass.Main) }
                }
                val now = SystemClock.uptimeMillis()
                event?.changes?.forEach { change ->
                    val id = change.id.value
                    val x = change.position.x / dp
                    val y = change.position.y / dp
                    when {
                        change.changedToDownIgnoreConsumed() -> controller.down(id, x, y, now)
                        change.changedToUpIgnoreConsumed() -> controller.up(id, x, y, now)
                        change.pressed && change.positionChanged() -> controller.move(id, x, y, now)
                    }
                    change.consume()
                }
                controller.tick(SystemClock.uptimeMillis())
            }
        } finally {
            controller.cancelAll()
        }
    }
}

// ---------------------------------------------------------------- drawing

private class KeyIcons(
    val shift: VectorPainter,
    val shiftOn: VectorPainter,
    val capsLock: VectorPainter,
    val delete: VectorPainter,
    val globe: VectorPainter,
    val newline: VectorPainter,
    val send: VectorPainter,
    val search: VectorPainter,
    val go: VectorPainter,
    val done: VectorPainter,
    val previous: VectorPainter
)

private enum class Face { LETTER, SPECIAL, ENGAGED, PROMINENT }

private fun faceOf(key: LaidOutKey, state: KeySurfaceState, lastRow: Int): Face = when (val k = key.key) {
    is KeyboardKey.Character ->
        // Gboard draws the punctuation beside space like the other controls.
        if (key.row == lastRow && k.value.length <= 2) Face.SPECIAL else Face.LETTER
    KeyboardKey.Space -> Face.LETTER
    KeyboardKey.Shift -> if (state.shiftMode != ShiftState.Mode.OFF) Face.ENGAGED else Face.SPECIAL
    KeyboardKey.Return -> if (state.returnProminent && state.returnEnabled) Face.PROMINENT else Face.SPECIAL
    else -> Face.SPECIAL
}

private fun DrawScope.drawPage(
    state: KeySurfaceState,
    theme: KeyboardTheme,
    visual: TouchVisual,
    measurer: TextMeasurer,
    icons: KeyIcons
) {
    val dp = density
    val lastRow = state.layout.keys.maxOfOrNull { it.row } ?: 0
    val keyAlpha = if (state.dimmed) 0.45f else 1f
    val labelAlpha = if (visual.trackpad) 0.25f else 1f
    val radius = CornerRadius(if (state.layout.rowMetrics.key >= 44f) 7f * dp else 6f * dp)
    val shifted = state.shiftMode != ShiftState.Mode.OFF
    val compactLetters = state.layout.page.columns >= 11

    state.layout.keys.forEachIndexed { index, key ->
        val face = faceOf(key, state, lastRow)
        val pressed = index in visual.pressed && !state.dimmed
        val fill = when (face) {
            Face.LETTER -> if (pressed) theme.letterKeyPressed else theme.letterKey
            Face.SPECIAL -> if (pressed) theme.specialKeyPressed else theme.specialKey
            Face.ENGAGED -> theme.engagedKey
            Face.PROMINENT -> if (pressed) theme.accent.copy(alpha = 0.72f) else theme.accent
        }
        val content = when (face) {
            Face.LETTER, Face.SPECIAL -> theme.primaryText
            Face.ENGAGED -> theme.engagedKeyGlyph
            Face.PROMINENT -> Color.White
        }.copy(alpha = labelAlpha)

        val frame = key.frame
        val topLeft = Offset(frame.left * dp, frame.top * dp)
        val size = Size(frame.width * dp, frame.height * dp)
        // A hard 1dp bottom edge rather than a blur: the native keyboards' weight,
        // and no offscreen pass per key.
        drawRoundRect(theme.keyShadow, topLeft + Offset(0f, 1f * dp), size, radius, alpha = keyAlpha)
        drawRoundRect(fill, topLeft, size, radius, alpha = keyAlpha)

        val center = Offset((frame.left + frame.width / 2f) * dp, (frame.top + frame.height / 2f) * dp)
        when (val k = key.key) {
            is KeyboardKey.Character -> {
                val letter = k.value.firstOrNull()?.isLetter() == true
                val text = if (shifted && letter && state.layout.page.plane == KeyboardPlane.LETTERS) {
                    k.value.uppercase()
                } else {
                    k.value
                }
                val fontSize = when {
                    key.row == lastRow -> 22f
                    compactLetters -> 21f
                    else -> 23f
                }
                drawCentered(measurer, text, center, TextStyle(fontSize = fontSize.sp, color = content), keyAlpha)
                key.hint?.let { hint ->
                    drawCorner(measurer, hint, frame, theme.secondaryText.copy(alpha = labelAlpha), keyAlpha)
                }
            }
            KeyboardKey.Space -> drawCentered(
                measurer, state.spaceCaption, center,
                TextStyle(fontSize = 13.sp, color = theme.secondaryText.copy(alpha = labelAlpha)), keyAlpha
            )
            KeyboardKey.Shift -> drawIcon(
                when (state.shiftMode) {
                    ShiftState.Mode.OFF -> icons.shift
                    ShiftState.Mode.ONCE -> icons.shiftOn
                    ShiftState.Mode.LOCKED -> icons.capsLock
                },
                center, 22f, content, keyAlpha
            )
            KeyboardKey.Backspace -> drawIcon(icons.delete, center, 22f, content, keyAlpha)
            KeyboardKey.Globe -> drawIcon(icons.globe, center, 21f, content, keyAlpha)
            KeyboardKey.Layout -> drawCentered(
                measurer, state.labels.badge, center,
                TextStyle(fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = content), keyAlpha
            )
            is KeyboardKey.Plane -> drawCentered(
                measurer, state.labels.planeKey(k.target), center,
                TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Medium, color = content), keyAlpha
            )
            KeyboardKey.Return -> {
                val painter = when (state.returnFace) {
                    ReturnFace.NEWLINE -> icons.newline
                    ReturnFace.SEND -> icons.send
                    ReturnFace.SEARCH -> icons.search
                    ReturnFace.GO, ReturnFace.NEXT -> icons.go
                    ReturnFace.PREVIOUS -> icons.previous
                    ReturnFace.DONE -> icons.done
                }
                val alpha = if (state.returnEnabled) keyAlpha else keyAlpha * 0.4f
                drawIcon(painter, center, 22f, content, alpha)
            }
        }
    }

    // The balloon over the key being typed.
    visual.preview?.let { index ->
        val key = state.layout.keys.getOrNull(index) ?: return@let
        val character = (key.key as? KeyboardKey.Character)?.value ?: return@let
        val text = if (shifted && character.firstOrNull()?.isLetter() == true &&
            state.layout.page.plane == KeyboardPlane.LETTERS
        ) character.uppercase() else character
        val width = maxOf(key.frame.width + 14f, 44f)
        val height = key.frame.height + 10f
        val left = (key.frame.centerX - width / 2f).coerceIn(1f, state.layout.width - width - 1f)
        val top = key.frame.top - height - 5f
        val topLeft = Offset(left * dp, top * dp)
        val size = Size(width * dp, height * dp)
        drawRoundRect(theme.keyShadow, topLeft + Offset(0f, 1.5f * dp), size, CornerRadius(9f * dp))
        drawRoundRect(theme.letterKey, topLeft, size, CornerRadius(9f * dp))
        drawCentered(
            measurer, text, Offset((left + width / 2f) * dp, (top + height / 2f) * dp),
            TextStyle(fontSize = 30.sp, color = theme.primaryText), 1f
        )
    }

    // Long-press alternates.
    visual.panel?.let { panel ->
        val topLeft = Offset(panel.left * dp, panel.top * dp)
        val size = Size((panel.right - panel.left) * dp, panel.cellHeight * dp)
        drawRoundRect(theme.keyShadow, topLeft + Offset(0f, 1.5f * dp), size, CornerRadius(9f * dp))
        drawRoundRect(theme.letterKey, topLeft, size, CornerRadius(9f * dp))
        panel.options.forEachIndexed { i, option ->
            val cellLeft = panel.left + i * panel.cellWidth
            val selected = i == panel.selected
            if (selected) {
                drawRoundRect(
                    theme.accent,
                    Offset((cellLeft + 2f) * dp, (panel.top + 2f) * dp),
                    Size((panel.cellWidth - 4f) * dp, (panel.cellHeight - 4f) * dp),
                    CornerRadius(7f * dp)
                )
            }
            val fontSize = if (option.length > 2) 15f else 22f
            drawCentered(
                measurer, option,
                Offset((cellLeft + panel.cellWidth / 2f) * dp, (panel.top + panel.cellHeight / 2f) * dp),
                TextStyle(fontSize = fontSize.sp, color = if (selected) Color.White else theme.primaryText), 1f
            )
        }
    }
}

private fun DrawScope.drawCentered(measurer: TextMeasurer, text: String, center: Offset, style: TextStyle, alpha: Float) {
    if (text.isEmpty()) return
    val result = measurer.measure(text, style, maxLines = 1)
    drawText(
        result,
        topLeft = Offset(center.x - result.size.width / 2f, center.y - result.size.height / 2f),
        alpha = alpha
    )
}

private fun DrawScope.drawCorner(measurer: TextMeasurer, text: String, frame: KeyBox, color: Color, alpha: Float) {
    val result = measurer.measure(text, TextStyle(fontSize = 10.sp, color = color), maxLines = 1)
    drawText(
        result,
        topLeft = Offset((frame.right - 3.5f) * density - result.size.width, (frame.top + 1.5f) * density),
        alpha = alpha
    )
}

private fun DrawScope.drawIcon(painter: VectorPainter, center: Offset, sizeDp: Float, color: Color, alpha: Float) {
    val side = sizeDp * density
    translate(center.x - side / 2f, center.y - side / 2f) {
        with(painter) {
            draw(Size(side, side), alpha = alpha, colorFilter = ColorFilter.tint(color))
        }
    }
}

private fun describe(key: LaidOutKey, state: KeySurfaceState): String = when (val k = key.key) {
    is KeyboardKey.Character ->
        if (state.shiftMode != ShiftState.Mode.OFF) k.value.uppercase() else k.value
    KeyboardKey.Shift -> if (state.shiftMode == ShiftState.Mode.LOCKED) state.labels.capsLock else state.labels.shift
    KeyboardKey.Backspace -> state.labels.delete
    KeyboardKey.Space -> state.labels.spaceDescription
    KeyboardKey.Return -> state.labels.returnDescription(state.returnFace)
    KeyboardKey.Globe -> state.labels.nextKeyboard
    KeyboardKey.Layout -> "${state.labels.switchLayout}, ${state.labels.badge}"
    is KeyboardKey.Plane -> state.labels.planeKey(k.target)
}
