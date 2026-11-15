import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

/**
 * Push notifications (Firebase Cloud Messaging).
 *
 * app/google-services.json holds the Firebase project's client configuration.
 * It contains public client identifiers and is version-controlled. With
 * the file present the Google Services plugin turns it into resources and
 * FirebaseApp initialises at startup; without it the plugin is not applied,
 * the build still succeeds, and push is simply unavailable at runtime
 * (PushSupport checks FirebaseApp.getApps before touching FirebaseMessaging).
 */
if (file("google-services.json").exists()) {
    apply(plugin = "com.google.gms.google-services")
}

/**
 * "Continue with Google": the OAuth client of type *Web application* from
 * Google Cloud Console. This public identifier ships in every APK. First match wins:
 *   -Paireply.googleWebClientId=… · aireply.googleWebClientId in
 *   local.properties or ~/.gradle/gradle.properties · GOOGLE_WEB_CLIENT_ID env
 *   · the production Web client below.
 * An explicit empty override remains empty: debug and static checks can run
 * without Google, while release APK/AAB packaging rejects missing setup.
 */
val googleWebClientId: String = run {
    val local = Properties().apply {
        val file = rootProject.file("local.properties")
        if (file.exists()) file.inputStream().use { load(it) }
    }
    val value = (findProperty("aireply.googleWebClientId") as String?)
        ?: local.getProperty("aireply.googleWebClientId")
        ?: System.getenv("GOOGLE_WEB_CLIENT_ID")
        ?: "307300959376-50l6atnfem2hsi62uguhm9hisrncnlh8.apps.googleusercontent.com"
    value.trim().also {
        require(it.isEmpty() || Regex("[0-9A-Za-z._-]+\\.apps\\.googleusercontent\\.com").matches(it)) {
            "aireply.googleWebClientId must look like <id>.apps.googleusercontent.com"
        }
    }
}

// A Google client ID does not encode its type. Catch the known iOS client
// from this repository; the actual Web client must still be checked against
// GOOGLE_CLIENT_ID_WEB on the backend and Google Cloud Console.
val knownIosGoogleClientId = rootProject.file("../AI-Reply/Config/GoogleSignIn/GoogleOAuth-Info.plist")
    .takeIf { it.exists() }
    ?.readText()
    ?.let { Regex("<key>CLIENT_ID</key>\\s*<string>([^<]+)</string>").find(it)?.groupValues?.get(1) }

/**
 * versionCode 7 / versionName "1.2.3" unless overridden for a store upload, so
 * every Play upload gets a higher number without editing this file:
 *   ./gradlew bundleRelease -Paireply.versionCode=8 -Paireply.versionName=1.2.4
 * A value that is not a positive whole number (Play's ceiling is 2100000000),
 * or an empty name, stops the build rather than uploading a wrong one.
 */
val appVersionCode: Int = (findProperty("aireply.versionCode") as String?)?.trim()?.let { raw ->
    requireNotNull(raw.toIntOrNull()?.takeIf { it in 1..2_100_000_000 }) {
        "aireply.versionCode must be a whole number from 1 to 2100000000, got \"$raw\""
    }
} ?: 7

val appVersionName: String = (findProperty("aireply.versionName") as String?)?.trim()?.also { name ->
    require(name.isNotEmpty()) { "aireply.versionName must not be empty" }
} ?: "1.2.3"

// An explicit build choice, shared by every transport and local store. Keep the
// package unchanged so license testers can use the existing Google Play products.
val backendEnvironment = (findProperty("aireply.backend") as String?)?.trim() ?: "production"
val backendBaseUrl = when (backendEnvironment) {
    "production" -> "https://ai-reply.kz"
    "sandbox" -> "https://ai-reply.kz/sandbox"
    else -> error("aireply.backend must be production or sandbox")
}

android {
    namespace = "kz.yerek.aireply"
    compileSdk = 36

    defaultConfig {
        applicationId = "kz.yerek.aireply"
        minSdk = 26
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersionName

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // The languages the product ships. Listing them keeps the APK
        // free of the ~70 locales AndroidX would otherwise drag in, and is what
        // res/xml/locales_config.xml declares to the system.
        resourceConfigurations += listOf("en", "ru", "kk", "uz")

        buildConfigField("String", "GOOGLE_WEB_CLIENT_ID", "\"$googleWebClientId\"")
        buildConfigField("String", "BACKEND_ENVIRONMENT", "\"$backendEnvironment\"")
        buildConfigField("String", "BACKEND_BASE_URL", "\"$backendBaseUrl\"")
        manifestPlaceholders["appLabel"] = if (backendEnvironment == "sandbox") "AI Reply Sandbox" else "@string/app_name"
    }

    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            // No signing config is checked in on purpose: a keystore in a
            // repository is a shipped secret. Android Studio ▸ Build ▸
            // Generate Signed Bundle supplies it at release time.
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
        freeCompilerArgs += listOf(
            "-opt-in=kotlin.RequiresOptIn",
            "-Xjvm-default=all"
        )
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }

    // JVM tests drive the keyboard's controllers, which log through
    // android.util.Log in debug builds; the stub answers instead of throwing.
    testOptions {
        unitTests.isReturnDefaultValues = true
    }

    lint {
        warningsAsErrors = false
        abortOnError = true
        // The keyboard deliberately reads the clipboard; the check that flags
        // any clipboard access has no way to see that it happens only on an
        // explicit tap, so it is disabled here and the rule is enforced by
        // ContextTextProvider being the single place that touches it.
        disable += listOf("UnusedResources")
        // Uzbek covers every production flow; older, rarely seen text falls
        // back to English on purpose. LocalizationParityTest enforces full
        // Russian and Kazakh coverage and the Uzbek entry points, so lint
        // reports the remaining Uzbek gaps without failing the build.
        warning += listOf("MissingTranslation")
        checkReleaseBuilds = true
    }
}

/**
 * Static checks and unit tests do not need external sign-in configuration.
 * Packaging an APK/AAB does: warn about Firebase and validate Google before
 * producing an artifact that could be uploaded with a missing sign-in method.
 */
val releaseSetupCheck = tasks.register("checkReleaseSetup") {
    val missingFirebase = !file("google-services.json").exists()
    val missingGoogleClientId = googleWebClientId.isEmpty()
    doLast {
        if (missingFirebase) {
            logger.warn(
                "WARNING: Firebase setup pending: app/google-services.json is not there yet, so this " +
                    "release build has no push notifications (FCM). Add it before uploading to Google Play " +
                    "(docs/FIREBASE_SETUP.md)."
            )
        }
        if (missingGoogleClientId) {
            logger.warn(
                "WARNING: aireply.googleWebClientId is empty. Google sign-in is unavailable; " +
                    "release APK/AAB packaging will fail. Set it in local.properties, " +
                    "~/.gradle/gradle.properties or GOOGLE_WEB_CLIENT_ID before uploading to Google Play."
            )
        }
    }
}
tasks.matching { it.name == "preReleaseBuild" }.configureEach { dependsOn(releaseSetupCheck) }

val releaseGoogleSignInCheck = tasks.register("checkReleaseGoogleSignIn") {
    val clientId = googleWebClientId
    val iosClientId = knownIosGoogleClientId
    inputs.property("googleWebClientId", clientId)
    inputs.property("knownIosGoogleClientId", iosClientId.orEmpty())
    doLast {
        check(clientId.isNotEmpty()) {
            "Release packaging requires the Google OAuth Web application client ID. " +
                "Set aireply.googleWebClientId in local.properties or pass " +
                "-Paireply.googleWebClientId=<web-client-id>.apps.googleusercontent.com " +
                "(or GOOGLE_WEB_CLIENT_ID). It must match backend GOOGLE_CLIENT_ID_WEB."
        }
        check(clientId != iosClientId) {
            "aireply.googleWebClientId is the repository's iOS OAuth client ID. " +
                "Use the Web application client ID that matches backend GOOGLE_CLIENT_ID_WEB."
        }
    }
}

// Guard the packaging tasks themselves, including Android Studio's signed
// bundle flow. lintRelease and testReleaseUnitTest remain usable without IDs.
tasks.matching {
    it.name in setOf("packageRelease", "packageReleaseBundle", "signReleaseBundle")
}.configureEach { dependsOn(releaseGoogleSignInCheck) }

// For normal distribution commands, stop before compilation as well. This
// uses start parameters instead of TaskExecutionGraph callbacks, preserving
// Gradle's configuration cache.
val releaseArtifactRequested = gradle.startParameter.taskNames.any {
    it.substringAfterLast(':') in setOf(
        "assemble", "bundle", "build", "assembleRelease", "bundleRelease",
        "packageRelease", "packageReleaseBundle", "signReleaseBundle"
    )
}
if (releaseArtifactRequested) {
    tasks.matching { it.name == "preReleaseBuild" }.configureEach { dependsOn(releaseGoogleSignInCheck) }
}

// LocalizationParityTest reads the string resources straight from src/main/res,
// which Gradle cannot see. Declared as an input, a change to a translation alone
// re-runs the unit tests instead of reporting an up-to-date or cached pass.
tasks.withType<Test>().configureEach {
    inputs.dir("src/main/res")
        .withPropertyName("stringResources")
        .withPathSensitivity(PathSensitivity.RELATIVE)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.savedstate)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.navigation.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons.extended)

    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services.auth)
    implementation(libs.googleid)

    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.messaging)

    implementation(libs.billing.ktx)
    implementation(libs.play.app.update)

    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(platform(libs.androidx.compose.bom))
}
