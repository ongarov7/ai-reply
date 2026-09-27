plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "kz.yerek.aireply"
    compileSdk = 35

    defaultConfig {
        applicationId = "kz.yerek.aireply"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "1.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // The languages the product ships. Listing them keeps the APK
        // free of the ~70 locales AndroidX would otherwise drag in, and is what
        // res/xml/locales_config.xml declares to the system.
        resourceConfigurations += listOf("en", "ru", "kk", "uz")
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

    lint {
        warningsAsErrors = false
        abortOnError = true
        // The keyboard deliberately reads the clipboard; the check that flags
        // any clipboard access has no way to see that it happens only on an
        // explicit tap, so it is disabled here and the rule is enforced by
        // ContextTextProvider being the single place that touches it.
        disable += listOf("UnusedResources")
        checkReleaseBuilds = true
    }
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

    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(platform(libs.androidx.compose.bom))
}
