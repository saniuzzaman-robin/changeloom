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

    // Mirrors :androidApp's flavors so each env gets its own profile tasks.
    flavorDimensions += "env"
    productFlavors {
        create("staging") { dimension = "env" }
        create("prod") { dimension = "env" }
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
