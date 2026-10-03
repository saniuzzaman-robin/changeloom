plugins {
    alias(libs.plugins.android.test)
    alias(libs.plugins.baselineprofile)
}

// Macrobenchmarks and the baseline profile generator for :androidApp. They drive the installed app, so the device
// must be signed in with followed topics and stories (see Journeys.kt). Always pass
// -Pandroid.injected.androidTest.leaveApksInstalledAfterRun=true: otherwise the run uninstalls the app, and its sign-in
// with it. On an emulator, benchmarks also need
// -Pandroid.testInstrumentationRunnerArguments.androidx.benchmark.suppressErrors=EMULATOR.
android {
    namespace = "dev.changeloom.baselineprofile"
    compileSdk = 37

    defaultConfig {
        minSdk = 28
        targetSdk = 36
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    targetProjectPath = ":androidApp"

    buildFeatures {
        buildConfig = true
    }

    // Mirrors :androidApp's flavors so each env gets its own profile tasks. TARGET_APP_ID is that flavor's
    // application id (staging adds a suffix).
    flavorDimensions += "env"
    productFlavors {
        create("staging") {
            dimension = "env"
            buildConfigField("String", "TARGET_APP_ID", "\"dev.changeloom.android.staging\"")
        }
        create("prod") {
            dimension = "env"
            buildConfigField("String", "TARGET_APP_ID", "\"dev.changeloom.android\"")
        }
    }
}

baselineProfile {
    useConnectedDevices = true
}

dependencies {
    implementation(libs.androidx.test.ext.junit)
    implementation(libs.androidx.test.uiautomator)
    implementation(libs.androidx.benchmark.macro.junit4)
}
