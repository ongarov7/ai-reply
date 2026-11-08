package kz.yerek.aireply.keyboard.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathBuilder
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp

/**
 * The shift key's three faces: the house-shaped arrow, hollow, filled, and
 * filled over a bar for caps lock. Material's icon set has no shift arrow of
 * this shape, and the keys are tinted at draw time, so the colour here does
 * not matter.
 */
internal object KeyGlyphs {

    private val outline: PathBuilder.() -> Unit = {
        moveTo(12f, 4f)
        lineTo(3.6f, 12.7f)
        horizontalLineTo(8.2f)
        verticalLineTo(19.6f)
        horizontalLineTo(15.8f)
        verticalLineTo(12.7f)
        horizontalLineTo(20.4f)
        close()
    }

    val Shift: ImageVector = glyph("KeyShift") {
        path(
            stroke = SolidColor(Color.Black),
            strokeLineWidth = 1.8f,
            strokeLineJoin = StrokeJoin.Round,
            strokeLineCap = StrokeCap.Round,
            pathBuilder = outline
        )
    }

    val ShiftFilled: ImageVector = glyph("KeyShiftFilled") {
        path(
            fill = SolidColor(Color.Black),
            stroke = SolidColor(Color.Black),
            strokeLineWidth = 1.8f,
            strokeLineJoin = StrokeJoin.Round,
            pathBuilder = outline
        )
    }

    val CapsLock: ImageVector = glyph("KeyCapsLock") {
        path(
            fill = SolidColor(Color.Black),
            stroke = SolidColor(Color.Black),
            strokeLineWidth = 1.6f,
            strokeLineJoin = StrokeJoin.Round
        ) {
            moveTo(12f, 3.2f)
            lineTo(4.4f, 11f)
            horizontalLineTo(8.6f)
            verticalLineTo(15.8f)
            horizontalLineTo(15.4f)
            verticalLineTo(11f)
            horizontalLineTo(19.6f)
            close()
        }
        path(fill = SolidColor(Color.Black)) {
            moveTo(8f, 18.4f)
            horizontalLineTo(16f)
            verticalLineTo(21f)
            horizontalLineTo(8f)
            close()
        }
    }

    private fun glyph(name: String, content: ImageVector.Builder.() -> Unit): ImageVector =
        ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f).apply(content).build()
}
