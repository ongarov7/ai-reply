package kz.yerek.aireply

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * What the app's one Activity does on the way back to the front.
 *
 * Қосымшаға қайтқанда: тіркелгі қайта оқылады.
 */
class AppResumeTest {

    private val onResume: String by lazy {
        File("src/main/java/kz/yerek/aireply/MainActivity.kt").readText()
            .substringAfter("override fun onResume()")
            .substringBefore("override fun onStop()")
    }

    /**
     * A website payment can change the shared balance while the app is away,
     * and a quota or plan notification must open on current numbers: Android
     * always resumes the Activity after delivering a tapped notification.
     */
    @Test
    fun `a return to the app reads the account again once start-up has`() {
        assertTrue(onResume, "services.account.refresh()" in onResume)
        assertTrue(onResume, "account.bootstrapComplete && account.isSignedIn" in onResume)
    }
}
