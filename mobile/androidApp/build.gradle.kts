import com.android.build.api.variant.BuildConfigField
import com.google.firebase.crashlytics.buildtools.gradle.CrashlyticsExtension
import com.google.gms.googleservices.GoogleServicesPlugin.GoogleServicesPluginConfig
import com.google.gms.googleservices.GoogleServicesPlugin.MissingGoogleServicesStrategy
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.baselineprofile)
}

// Hosted environments, one product flavor each (see deploy/README.md).
val envs = listOf("staging", "prod")

// Firebase config, never committed: src/<env>/google-services.json from each env's Firebase project,
// else the shared google-services.json next to this file. Each file must have a client for its flavor's
// application id: dev.changeloom.android.staging (staging project) or dev.changeloom.android (prod project).
// Without config a variant still builds, but the app crashes at launch (Firebase is not
// initialized); release builds require it (see verify<Env>ReleaseConfig below).
val googleServicesFiles = envs.associateWith { env ->
    file("src/$env/google-services.json").takeIf { it.exists() } ?: file("google-services.json")
}
if (googleServicesFiles.values.any { it.exists() }) {
    apply(plugin = libs.plugins.google.services.get().pluginId)
    // Crashlytics reads the Firebase app id, so it needs the same config.
    apply(plugin = libs.plugins.firebase.crashlytics.get().pluginId)
    configure<GoogleServicesPluginConfig> {
        missingGoogleServicesStrategy = MissingGoogleServicesStrategy.WARN
    }
}

// The api each env talks to: -Pchangeloom.<env>.apiBaseUrl (or ~/.gradle/gradle.properties), else
// changeloom.apiBaseUrl, else the local api from the emulator. Release builds require an https URL.
val localApiBaseUrl = "http://10.0.2.2:8080"
fun apiBaseUrl(env: String): String =
    providers.gradleProperty("changeloom.$env.apiBaseUrl")
        .orElse(providers.gradleProperty("changeloom.apiBaseUrl"))
        .orElse(localApiBaseUrl)
        .get()

// AdMob: Google's sample ids for every build but prod release, so real ads are never served to debug or
// staging builds (invalid traffic). Prod release needs -Pchangeloom.prod.admobAppId and
// -Pchangeloom.prod.admobNativeAdUnitId from the AdMob console (see deploy/README.md).
val admobTestAppId = "ca-app-pub-3940256099942544~3347511713"
val admobTestNativeAdUnitId = "ca-app-pub-3940256099942544/2247696110"
fun admobProperty(name: String): String? =
    providers.gradleProperty("changeloom.prod.$name").orNull?.takeIf { it.isNotBlank() }

// Debug builds only: the hashed device id UMP logs, to test the EEA consent form from anywhere.
val umpTestDeviceId = providers.gradleProperty("changeloom.umpTestDeviceId").orNull.orEmpty()

// CI names staging builds after the last release and the build: -Pchangeloom.stagingBuild=<run>+<commit> makes
// versionName 1.2.3-staging.45+abc1234 (with -Pchangeloom.versionName=1.2.3). Semver pre-release/build characters only.
val stagingBuild = providers.gradleProperty("changeloom.stagingBuild").orNull?.also {
    if (!Regex("[0-9A-Za-z.+-]+").matches(it)) throw GradleException("changeloom.stagingBuild must look like 45+abc1234, got \"$it\"")
}

// Release signing (both envs use the same upload key), never committed: mobile/keystore.properties
// with storeFile (relative to mobile/), storePassword, keyAlias and keyPassword.
val keystorePropertiesFile = rootProject.file("keystore.properties")
val keystoreProperties = Properties().apply {
    if (keystorePropertiesFile.exists()) keystorePropertiesFile.inputStream().use(::load)
}
fun keystoreProperty(key: String): String =
    keystoreProperties.getProperty(key)?.takeIf { it.isNotBlank() }
        ?: throw GradleException("$key is missing from ${keystorePropertiesFile.path} (see deploy/README.md)")

// Crashlytics uploads release builds' R8 mapping files so their crashes are deobfuscated. CI's compile-only build
// (placeholder Firebase config) turns it off with -Pchangeloom.crashlyticsMappingUpload=false.
val crashlyticsMappingUpload = providers.gradleProperty("changeloom.crashlyticsMappingUpload").orNull != "false"
fun ExtensionAware.crashlyticsMappingUpload(enabled: Boolean) {
    // Absent when the Crashlytics plugin isn't applied (no Firebase config).
    extensions.findByType(CrashlyticsExtension::class.java)?.mappingFileUploadEnabled = enabled
}

android {
    namespace = "dev.changeloom.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "dev.changeloom.android"
        minSdk = 26
        targetSdk = 36
        // CI sets both from the release workflow (-Pchangeloom.versionCode, -Pchangeloom.versionName).
        versionCode = providers.gradleProperty("changeloom.versionCode").orNull?.let {
            it.toIntOrNull()?.takeIf { code -> code > 0 }
                ?: throw GradleException("changeloom.versionCode must be a positive integer, got \"$it\"")
        } ?: 1
        versionName = providers.gradleProperty("changeloom.versionName").orNull ?: "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // ViewModel tests run on the JVM; android.util.Log (AppLog in debug) returns defaults there.
    testOptions.unitTests.isReturnDefaultValues = true

    flavorDimensions += "env"
    productFlavors {
        create("staging") {
            dimension = "env"
            // Its own app, so staging and prod install side by side and can register the same signing keys
            // (Google sign-in needs each package + SHA-1 pair to be unique across projects).
            applicationIdSuffix = ".staging"
            versionNameSuffix = "-staging" + stagingBuild?.let { ".$it" }.orEmpty()
        }
        create("prod") {
            dimension = "env"
        }
    }
    productFlavors.configureEach {
        buildConfigField("String", "API_BASE_URL", "\"${apiBaseUrl(name)}\"")
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    // Per-developer debug key so the SHA-1 registered in Firebase is unique to this app; falls back to the default debug key.
    val changeloomDebugKeystore = file("${System.getProperty("user.home")}/.android/changeloom-debug.keystore")
    if (changeloomDebugKeystore.exists()) {
        signingConfigs.getByName("debug") {
            storeFile = changeloomDebugKeystore
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
    }

    if (keystorePropertiesFile.exists()) {
        signingConfigs.create("release") {
            storeFile = rootProject.file(keystoreProperty("storeFile"))
            storePassword = keystoreProperty("storePassword")
            keyAlias = keystoreProperty("keyAlias")
            keyPassword = keystoreProperty("keyPassword")
        }
    }

    // Crashlytics, Analytics and Performance collect only in release builds (see AndroidManifest.xml).
    buildTypes {
        debug {
            manifestPlaceholders["firebaseCollectionEnabled"] = "false"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            crashlyticsMappingUpload(crashlyticsMappingUpload)
            signingConfig = signingConfigs.findByName("release")
            manifestPlaceholders["firebaseCollectionEnabled"] = "true"
        }
    }
}

composeCompiler {
    // Only for the models the compiler reports flagged as unstable params on hot paths.
    stabilityConfigurationFiles.add(layout.projectDirectory.file("compose_stability.conf"))
}

// CI builds release variants without a signing key: -Pchangeloom.allowUnsignedRelease=true skips
// only the signing check below, and the build output is unsigned.
val allowUnsignedRelease = providers.gradleProperty("changeloom.allowUnsignedRelease").orNull == "true"

// The baselineprofile plugin creates these from release, for local benchmarking and profile generation only.
val benchmarkBuildTypes = listOf("benchmarkRelease", "nonMinifiedRelease")

// A release build fails early, before compiling, unless its env is fully configured.
androidComponents {
    // Runs after the baselineprofile plugin has created its build types. The debug key keeps the installed debug
    // app's data (the benchmarks need a signed-in account), and debug's network config allows the local api.
    finalizeDsl { android ->
        benchmarkBuildTypes.forEach { name ->
            android.buildTypes.getByName(name) {
                signingConfig = android.signingConfigs.getByName("debug")
                manifestPlaceholders["firebaseCollectionEnabled"] = "false"
                crashlyticsMappingUpload(false)
            }
            android.sourceSets.getByName(name).res.srcDir("src/debug/res")
            // The App Check provider is per build type (src/debug, src/release); these build like release.
            android.sourceSets.getByName(name).kotlin.srcDir("src/release/kotlin")
        }
    }

    onVariants { variant ->
        val realAds = variant.flavorName == "prod" && variant.buildType == "release"
        val appId = admobProperty("admobAppId")?.takeIf { realAds } ?: admobTestAppId
        val nativeUnitId = admobProperty("admobNativeAdUnitId")?.takeIf { realAds } ?: admobTestNativeAdUnitId
        variant.manifestPlaceholders.put("admobAppId", appId)
        variant.buildConfigFields?.put("ADMOB_NATIVE_AD_UNIT_ID", BuildConfigField("String", "\"$nativeUnitId\"", null))
        val testDevice = umpTestDeviceId.takeIf { variant.buildType == "debug" }.orEmpty()
        variant.buildConfigFields?.put("UMP_TEST_DEVICE_ID", BuildConfigField("String", "\"$testDevice\"", null))
    }

    onVariants(selector().withBuildType("release")) { variant ->
        val env = variant.flavorName.orEmpty()
        val problems = buildList {
            val url = apiBaseUrl(env)
            if (!url.startsWith("https://")) {
                add("the api URL is $url: set changeloom.$env.apiBaseUrl to the $env Cloud Run URL")
            }
            // The shared fallback is staging's Firebase project, so prod must ship its own.
            val config = if (env == "prod") file("src/$env/google-services.json") else googleServicesFiles[env]
            if (config?.exists() != true) {
                add("src/$env/google-services.json is missing: download it from the $env Firebase project")
            }
            if (env == "prod" && (admobProperty("admobAppId") == null || admobProperty("admobNativeAdUnitId") == null)) {
                add("set changeloom.prod.admobAppId and changeloom.prod.admobNativeAdUnitId from the AdMob console")
            }
            if (!keystorePropertiesFile.exists() && !allowUnsignedRelease) {
                add("${keystorePropertiesFile.path} is missing, so the build would be unsigned")
            }
        }
        val variantName = variant.name.replaceFirstChar { it.uppercase() }
        val verify = tasks.register("verify${variantName}Config") {
            group = "verification"
            description = "Checks that the $env release has an https api URL, Firebase config, AdMob ids (prod) and a signing key."
            doLast {
                if (problems.isNotEmpty()) {
                    throw GradleException("$env release is not configured (see deploy/README.md):\n- " + problems.joinToString("\n- "))
                }
            }
        }
        tasks.named { it == "pre${variantName}Build" }.configureEach { dependsOn(verify) }
    }
}

dependencies {
    implementation(project(":shared"))
    implementation(platform(libs.compose.bom))
    implementation(platform(libs.firebase.bom))

    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.extended)
    implementation(libs.compose.ui.tooling.preview)
    debugImplementation(libs.compose.ui.tooling)
    debugImplementation(libs.leakcanary.android)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)

    implementation(libs.koin.android)
    implementation(libs.koin.compose)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.coroutines.play.services)
    implementation(libs.ktor.client.okhttp)
    implementation(libs.kotlinx.serialization.json)

    implementation(libs.firebase.auth)
    implementation(libs.firebase.messaging)
    implementation(libs.firebase.crashlytics)
    implementation(libs.firebase.analytics)
    implementation(libs.firebase.perf)
    implementation(libs.firebase.config)
    implementation(libs.play.services.ads)
    implementation(libs.google.ump)
    implementation(libs.play.app.update)
    implementation(libs.play.review)
    // App Check: Play Integrity in release builds, the debug provider (a token you allow-list) in debug builds.
    implementation(libs.firebase.appcheck.playintegrity)
    debugImplementation(libs.firebase.appcheck.debug)
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services)
    implementation(libs.googleid)

    implementation(libs.androidx.profileinstaller)
    implementation(libs.androidx.core.splashscreen)
    baselineProfile(project(":baselineprofile"))

    testImplementation(libs.kotlin.test.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.ktor.client.mock)
    androidTestImplementation(platform(libs.compose.bom))
    androidTestImplementation(libs.compose.ui.test.junit4)
    androidTestImplementation(libs.androidx.test.runner)
    debugImplementation(libs.compose.ui.test.manifest)
}
