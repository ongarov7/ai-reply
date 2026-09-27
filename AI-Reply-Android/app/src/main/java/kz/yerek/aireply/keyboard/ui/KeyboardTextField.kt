package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState

/**
 * A text field the KEYBOARD edits.
 *
 * WHY NOT `BasicTextField`. A normal text field asks the system for focus and
 * then expects an input method to serve it. Inside an input method that is
 * circular: there is no second keyboard, and requesting focus from an IME
 * window behaves differently across OEM builds. So this renders the text,
 * draws its own caret, takes edits from the keys through
 * [KeyboardTextFieldState], and places the caret where the user taps - which
 * is what makes editing ANY part of a reply possible.
 *
 * The field scrolls rather than grows: its height is decided by the panel
 * on content events (a new version), never by a keystroke, so the keyboard
 * does not move under the user's thumbs while they type.
 */
@Composable
fun KeyboardTextField(
    state: KeyboardTextFieldState,
    placeholder: String,
    textStyle: TextStyle,
    textColor: Color,
    placeholderColor: Color,
    caretColor: Color,
    isActive: Boolean,
    onTap: (Int) -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: Dp = 8.dp,
    accessibilityLabel: String? = null
) {
    var layout by remember { mutableStateOf<TextLayoutResult?>(null) }
    var caretVisible by remember { mutableStateOf(true) }
    var viewport by remember { mutableIntStateOf(0) }
    val scroll = rememberScrollState()
    val currentOnTap by rememberUpdatedState(onTap)
    val paddingPx = with(LocalDensity.current) { contentPadding.toPx() }

    // Blink only while this field is the one being typed into. A caret
    // blinking where the keys do not go is a lie about where text will land.
    LaunchedEffect(isActive, state.cursor, state.text) {
        if (!isActive) {
            caretVisible = false
            return@LaunchedEffect
        }
        caretVisible = true
        while (true) {
            delay(BLINK_MS)
            caretVisible = !caretVisible
        }
    }

    // Keep the caret in view as the user types or moves it.
    LaunchedEffect(isActive, state.cursor, state.text, layout, viewport) {
        if (!isActive || viewport <= 0) return@LaunchedEffect
        val result = layout ?: return@LaunchedEffect
        val cursor = state.cursor.coerceIn(0, state.text.length)
        val rect = runCatching { result.getCursorRect(cursor) }.getOrNull() ?: return@LaunchedEffect
        // Content coordinates: the text sits `paddingPx` into the scrolled box.
        val top = rect.top
        val bottom = rect.bottom + paddingPx * 2
        when {
            top < scroll.value -> scroll.scrollTo(top.toInt().coerceAtLeast(0))
            bottom > scroll.value + viewport -> scroll.scrollTo((bottom - viewport).toInt().coerceAtLeast(0))
        }
    }

    val showPlaceholder = state.text.isEmpty()

    Box(
        modifier = modifier
            .onSizeChanged { viewport = it.height }
            .then(
                if (accessibilityLabel != null) {
                    Modifier.semantics {
                        contentDescription = if (showPlaceholder) accessibilityLabel else state.text
                    }
                } else {
                    Modifier
                }
            )
            .pointerInput(state) {
                detectTapGestures { position ->
                    val result = layout
                    // The tap is in the field's frame; the text is scrolled
                    // and inset inside it.
                    val inset = contentPadding.toPx()
                    val local = Offset(position.x - inset, position.y - inset + scroll.value)
                    currentOnTap(if (result == null) state.text.length else result.getOffsetForPosition(local))
                }
            }
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(scroll)
                .padding(contentPadding)
        ) {
            BasicText(
                text = state.text.ifEmpty { placeholder },
                style = textStyle.copy(color = if (showPlaceholder) placeholderColor else textColor),
                onTextLayout = { layout = it },
                modifier = Modifier
                    .fillMaxWidth()
                    .drawWithContent {
                        drawContent()
                        if (!isActive || !caretVisible) return@drawWithContent
                        if (showPlaceholder) {
                            val height = layout?.getLineBottom(0) ?: (textStyle.fontSize.value * 1.3f * density)
                            drawRect(caretColor, Offset.Zero, Size(CARET_WIDTH.toPx(), height))
                            return@drawWithContent
                        }
                        val result = layout ?: return@drawWithContent
                        val cursor = state.cursor.coerceIn(0, state.text.length)
                        val rect = runCatching { result.getCursorRect(cursor) }.getOrNull()
                            ?: return@drawWithContent
                        drawRect(
                            color = caretColor,
                            topLeft = Offset(rect.left, rect.top),
                            size = Size(CARET_WIDTH.toPx(), rect.height)
                        )
                    }
            )
        }
    }
}

private val CARET_WIDTH = 2.dp
private const val BLINK_MS = 530L
