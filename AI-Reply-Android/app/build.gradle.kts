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
 * It is environment-specific and git-ignored, so it is never committed. With
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
 * Google Cloud Console. Not a secret (it ships in every APK), but it differs
 * per environment, so it is not checked in. First match wins:
 *   -Paireply.googleWebClientId=… · aireply.googleWebClientId in
 *   local.properties or ~/.gradle/gradle.properties · GOOGLE_WEB_CLIENT_ID env.
 * Empty builds fine and simply hides the Google button.
 */
val googleWebClientId: String = run {
    val local = Properties().apply {
        val file = rootProject.file("local.properties")
        if (file.exists()) file.inputStream().use { load(it) }
    }
    val value = (findProperty("aireply.googleWebClientId") as String?)
        ?: local.getProperty("aireply.googleWebClientId")
        ?: System.getenv("GOOGLE_WEB_CLIENT_ID")
        ?: ""
    value.trim().also {
        require(it.isEmpty() || Regex("[0-9A-Za-z._-]+\\.apps\\.googleusercontent\\.com").matches(it)) {
            "aireply.googleWebClientId must look like <id>.apps.googleusercontent.com"
        }
    }
}

/**
 * versionCode 1 / versionName "1.0" unless overridden for a store upload, so
 * every Play upload gets a higher number without editing this file:
 *   ./gradlew bundleRelease -Paireply.versionCode=7 -Paireply.versionName=1.0.6
 * A value that is not a positive whole number (Play's ceiling is 2100000000),
 * or an empty name, stops the build rather than uploading a wrong one.
 */
val appVersionCode: Int = (findProperty("aireply.versionCode") as String?)?.trim()?.let { raw ->
    requireNotNull(raw.toIntOrNull()?.takeIf { it in 1..2_100_000_000 }) {
        "aireply.versionCode must be a whole number from 1 to 2100000000, got \"$raw\""
    }
} ?: 1

val appVersionName: String = (findProperty("aireply.versionName") as String?)?.trim()?.also { name ->
    require(name.isNotEmpty()) { "aireply.versionName must not be empty" }
} ?: "1.0"

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
 * A release built without push or without Google sign-in still builds - both
 * are optional by design - but a store upload without them is almost always
 * a mistake, so every release build says so loudly. A warning, not a failure:
 * CI and local release checks keep working without the private files.
 */
val releaseSetupCheck = tasks.register("checkReleaseSetup") {
    val missingFirebase = !file("google-services.json").exists()
    val missingGoogleClientId = googleWebClientId.isEmpty()
    doLast {
        if (missingFirebase) {
            logger.warn(
                "WARNING: app/google-services.json is missing. This release build has no push " +
                    "notifications (FCM). Add the Firebase config before uploading to Google Play."
            )
        }
        if (missingGoogleClientId) {
            logger.warn(
                "WARNING: aireply.googleWebClientId is empty. This release build hides " +
                    "\"Continue with Google\". Set it in local.properties, ~/.gradle/gradle.properties " +
                    "or GOOGLE_WEB_CLIENT_ID before uploading to Google Play."
            )
        }
    }
}
tasks.matching { it.name == "preReleaseBuild" }.configureEach { dependsOn(releaseSetupCheck) }

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

    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(platform(libs.androidx.compose.bom))
}
