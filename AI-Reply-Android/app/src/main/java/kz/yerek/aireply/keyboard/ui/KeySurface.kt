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
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.SpaceBar
import androidx.compose.material.icons.outlined.EmojiEmotions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.asAndroidPath
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.toArgb
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
import kz.yerek.aireply.keyboard.KeyVisualStyle
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.layout.Box as KeyBox
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.LaidOutKey
import kz.yerek.aireply.keyboard.layout.LaidOutPanel
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
 *
 * A page's panels (the emoji grid and its categories) are Compose content
 * laid over the canvas by [panel], each in its whole touch area: a touch
 * there is the panel's and never reaches the keys.
 */
@Composable
fun KeySurface(
    state: KeySurfaceState,
    theme: KeyboardTheme,
    controller: KeySurfaceController,
    onAccessibilityKey: (KeyboardKey) -> Unit,
    modifier: Modifier = Modifier,
    panel: @Composable (LaidOutPanel) -> Unit = {}
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
            shift = rememberVectorPainter(KeyGlyphs.Shift),
            shiftOn = rememberVectorPainter(KeyGlyphs.ShiftFilled),
            capsLock = rememberVectorPainter(KeyGlyphs.CapsLock),
            delete = rememberVectorPainter(Icons.AutoMirrored.Outlined.Backspace),
            globe = rememberVectorPainter(Icons.Filled.Language),
            newline = rememberVectorPainter(Icons.AutoMirrored.Filled.KeyboardReturn),
            send = rememberVectorPainter(Icons.AutoMirrored.Filled.Send),
            search = rememberVectorPainter(Icons.Filled.Search),
            go = rememberVectorPainter(Icons.AutoMirrored.Filled.ArrowForward),
            done = rememberVectorPainter(Icons.Filled.Check),
            previous = rememberVectorPainter(Icons.AutoMirrored.Filled.ArrowBack),
            space = rememberVectorPainter(Icons.Filled.SpaceBar),
            emoji = rememberVectorPainter(Icons.Outlined.EmojiEmotions)
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

            // On top of everything, and owning every touch in its area -
            // a tap between two emoji must not type the nearest key.
            state.layout.panels.forEach { laid ->
                Box(
                    Modifier
                        .offset(laid.hitFrame.left.dp, laid.hitFrame.top.dp)
                        .size(laid.hitFrame.width.dp, laid.hitFrame.height.dp)
                        .pointerInput(Unit) {
                            awaitPointerEventScope {
                                while (true) awaitPointerEvent()
                            }
                        }
                ) {
                    panel(laid)
                }
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
    val previous: VectorPainter,
    /** The pads' space key: a narrow side key, with no language to name. */
    val space: VectorPainter,
    /** The letters' emoji key. */
    val emoji: VectorPainter
)

private enum class Face { LETTER, SPECIAL, ENGAGED, PROMINENT, DISABLED }

private fun faceOf(key: LaidOutKey, state: KeySurfaceState): Face = when (key.key) {
    // A pad says: digits like letters, its side columns like the controls.
    is KeyboardKey.Character -> key.style?.face ?: Face.LETTER
    KeyboardKey.Space -> key.style?.face ?: Face.LETTER
    KeyboardKey.Shift -> if (state.shiftMode != ShiftState.Mode.OFF) Face.ENGAGED else Face.SPECIAL
    KeyboardKey.Return -> when {
        !state.returnEnabled -> Face.DISABLED
        state.returnProminent -> Face.PROMINENT
        else -> Face.SPECIAL
    }
    else -> Face.SPECIAL
}

/** Keys whose only job is to put text somewhere, or take it away. */
private val KeyboardKey.editsText: Boolean
    get() = this is KeyboardKey.Character || this == KeyboardKey.Backspace ||
        this == KeyboardKey.Space || this == KeyboardKey.Return

private val KeyVisualStyle.face: Face
    get() = when (this) {
        KeyVisualStyle.LETTER -> Face.LETTER
        KeyVisualStyle.SPECIAL -> Face.SPECIAL
        KeyVisualStyle.ENGAGED -> Face.ENGAGED
        KeyVisualStyle.PROMINENT -> Face.PROMINENT
    }

private fun DrawScope.drawPage(
    state: KeySurfaceState,
    theme: KeyboardTheme,
    visual: TouchVisual,
    measurer: TextMeasurer,
    icons: KeyIcons
) {
    val dp = density
    val labelAlpha = if (visual.trackpad) 0f else 1f
    val radius = CornerRadius(if (state.layout.rowMetrics.key >= 40f) 6f * dp else 5f * dp)
    val shifted = state.shiftMode != ShiftState.Mode.OFF
    val pad = state.layout.page.plane.isPad
    val letterSize = letterFontSize(state.layout)

    state.layout.keys.forEachIndexed { index, key ->
        val face = faceOf(key, state)
        // Only keys that edit text dim: the rest still change the keyboard itself.
        val inert = state.dimmed && key.key.editsText
        val keyAlpha = if (inert) 0.45f else 1f
        val pressed = index in visual.pressed && !inert
        val fill = when (face) {
            Face.LETTER -> if (pressed) theme.letterKeyPressed else theme.letterKey
            Face.SPECIAL -> if (pressed) theme.specialKeyPressed else theme.specialKey
            Face.ENGAGED -> theme.engagedKey
            Face.PROMINENT -> if (pressed) theme.accent.copy(alpha = 0.7f) else theme.accent
            Face.DISABLED -> theme.specialKey
        }
        val content = when (face) {
            Face.LETTER, Face.SPECIAL -> theme.primaryText
            Face.ENGAGED -> theme.engagedKeyGlyph
            Face.PROMINENT -> Color.White
            Face.DISABLED -> theme.secondaryText
        }.let { it.copy(alpha = it.alpha * labelAlpha) }

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
                    // A pad's digits are a dialler's: large on large keys.
                    pad -> if (key.style == KeyVisualStyle.LETTER) 26f else 22f
                    k.value.length > 1 -> CONTROL_FONT
                    else -> letterSize
                }
                drawFitted(measurer, text, frame, center, TextStyle(fontSize = fontSize.sp, color = content), keyAlpha)
                key.hint?.let { hint ->
                    drawCorner(measurer, hint, frame, theme.secondaryText.copy(alpha = labelAlpha), keyAlpha)
                }
            }
            KeyboardKey.Space -> if (pad) {
                drawIcon(icons.space, center, 22f, content, keyAlpha)
            } else {
                drawFitted(
                    measurer, state.spaceCaption, frame, center,
                    TextStyle(fontSize = CONTROL_FONT.sp, color = content), keyAlpha
                )
            }
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
            KeyboardKey.Layout -> drawFitted(
                measurer, state.labels.badge, frame, center,
                TextStyle(fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = content), keyAlpha
            )
            is KeyboardKey.Plane -> if (k.target == KeyboardPlane.EMOJI) {
                // A face, drawn like the globe: an icon on a control key.
                drawIcon(icons.emoji, center, 22f, content, keyAlpha)
            } else {
                drawFitted(
                    measurer, state.labels.planeKey(k.target), frame, center,
                    TextStyle(fontSize = CONTROL_FONT.sp, color = content), keyAlpha
                )
            }
            KeyboardKey.Return -> {
                val caption = state.labels.returnCaption(state.returnFace)
                if (caption != null) {
                    drawFitted(measurer, caption, frame, center, TextStyle(fontSize = CONTROL_FONT.sp, color = content), keyAlpha)
                } else {
                    val painter = when (state.returnFace) {
                        ReturnFace.NEWLINE -> icons.newline
                        ReturnFace.SEND -> icons.send
                        ReturnFace.SEARCH -> icons.search
                        ReturnFace.GO, ReturnFace.NEXT -> icons.go
                        ReturnFace.PREVIOUS -> icons.previous
                        ReturnFace.DONE -> icons.done
                    }
                    drawIcon(painter, center, 22f, content, keyAlpha)
                }
            }
        }
    }

    // The balloon over the key being typed. Not on a pad: its keys are far
    // wider than a fingertip, and a balloon that size would cover a third of
    // the row above.
    visual.preview?.takeIf { !pad }?.let { index ->
        val key = state.layout.keys.getOrNull(index) ?: return@let
        val character = (key.key as? KeyboardKey.Character)?.value ?: return@let
        val text = if (shifted && character.firstOrNull()?.isLetter() == true &&
            state.layout.page.plane == KeyboardPlane.LETTERS
        ) character.uppercase() else character
        drawBalloon(key.frame, state.layout.width, theme, measurer, text)
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
            val fontSize = if (panel.options.any { it.length > 1 }) 17f else 26f
            drawFitted(
                measurer, option,
                KeyBox(cellLeft, panel.top, cellLeft + panel.cellWidth, panel.top + panel.cellHeight),
                Offset((cellLeft + panel.cellWidth / 2f) * dp, (panel.top + panel.cellHeight / 2f) * dp),
                TextStyle(fontSize = fontSize.sp, color = if (selected) Color.White else theme.primaryText), 1f
            )
        }
    }
}

/** The letters' size: a little smaller on narrow keys and on a phone on its side, as on the iOS keyboard. */
private fun letterFontSize(layout: PageLayout): Float {
    val width = layout.keys.firstOrNull { it.key is KeyboardKey.Character }?.frame?.width ?: 32f
    return when {
        layout.rowMetrics.key < 36f -> 20f
        width < 31f -> 22f
        else -> 23.5f
    }
}

/**
 * Centred text that shrinks to fit its key, down to half its size, rather
 * than spilling over the edges: "Отправить" on the return key, ".com" in its
 * cell of the long-press panel.
 */
private fun DrawScope.drawFitted(
    measurer: TextMeasurer,
    text: String,
    frame: KeyBox,
    center: Offset,
    style: TextStyle,
    alpha: Float
) {
    if (text.isEmpty()) return
    val available = (frame.width - 4f) * density
    val natural = measurer.measure(text, style, maxLines = 1)
    val scale = (available / natural.size.width.coerceAtLeast(1)).coerceIn(0.5f, 1f)
    val result = if (scale < 1f) measurer.measure(text, style.copy(fontSize = style.fontSize * scale), maxLines = 1) else natural
    drawText(
        result,
        topLeft = Offset(center.x - result.size.width / 2f, center.y - result.size.height / 2f),
        alpha = alpha
    )
}

/**
 * The balloon: the pressed letter, large, above the finger, growing out of
 * the key itself. It leans inwards at the edges and flattens under the top of
 * the area above the keys.
 */
private fun DrawScope.drawBalloon(key: KeyBox, pageWidth: Float, theme: KeyboardTheme, measurer: TextMeasurer, text: String) {
    val dp = density
    val neck = 10f
    val topWidth = minOf(maxOf(key.width * 1.5f, key.width + 18f), key.width + 30f)
    var topHeight = key.height + 6f
    val topLeft = (key.centerX - topWidth / 2f).coerceIn(1f, maxOf(1f, pageWidth - 1f - topWidth))
    var topTop = key.top - neck - topHeight
    val ceiling = -PersonaRowHeadroom + 1f
    if (topTop < ceiling) {
        topHeight = maxOf(key.height * 0.8f, topHeight - (ceiling - topTop))
        topTop = maxOf(ceiling, key.top - neck - topHeight)
    }
    val top = Rect(topLeft * dp, topTop * dp, (topLeft + topWidth) * dp, (topTop + topHeight) * dp)
    val base = Rect(key.left * dp, key.top * dp, key.right * dp, key.bottom * dp)
    val radius = 9f * dp
    val keyRadius = 6f * dp
    val neckBottom = base.top + minOf(neck * dp, base.height / 3f)
    val rise = neckBottom - top.bottom

    val path = Path().apply {
        moveTo(top.left, top.top + radius)
        arcTo(Rect(top.left, top.top, top.left + radius * 2, top.top + radius * 2), 180f, 90f, false)
        lineTo(top.right - radius, top.top)
        arcTo(Rect(top.right - radius * 2, top.top, top.right, top.top + radius * 2), 270f, 90f, false)
        lineTo(top.right, top.bottom)
        cubicTo(top.right, top.bottom + rise * 0.6f, base.right, top.bottom + rise * 0.4f, base.right, neckBottom)
        lineTo(base.right, base.bottom - keyRadius)
        arcTo(Rect(base.right - keyRadius * 2, base.bottom - keyRadius * 2, base.right, base.bottom), 0f, 90f, false)
        lineTo(base.left + keyRadius, base.bottom)
        arcTo(Rect(base.left, base.bottom - keyRadius * 2, base.left + keyRadius * 2, base.bottom), 90f, 90f, false)
        lineTo(base.left, neckBottom)
        cubicTo(base.left, top.bottom + rise * 0.4f, top.left, top.bottom + rise * 0.6f, top.left, top.bottom)
        close()
    }
    drawIntoCanvas { canvas ->
        val paint = android.graphics.Paint(android.graphics.Paint.ANTI_ALIAS_FLAG).apply {
            color = theme.letterKey.toArgb()
            setShadowLayer(3f * dp, 0f, 1f * dp, Color.Black.copy(alpha = 0.28f).toArgb())
        }
        canvas.nativeCanvas.drawPath(path.asAndroidPath(), paint)
    }
    val style = TextStyle(fontSize = (if (text.length > 1) 20f else 36f).sp, color = theme.primaryText)
    val result = measurer.measure(text, style, maxLines = 1)
    drawText(
        result,
        topLeft = Offset(top.center.x - result.size.width / 2f, top.center.y - result.size.height / 2f)
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

/** The control keys' captions: 123, ABC, space, return. */
private const val CONTROL_FONT = 16f

/** How far above the keys the balloon may reach: the persona row. */
private const val PersonaRowHeadroom = 48f

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
