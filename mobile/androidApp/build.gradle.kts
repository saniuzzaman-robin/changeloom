import com.google.gms.googleservices.GoogleServicesPlugin.GoogleServicesPluginConfig
import com.google.gms.googleservices.GoogleServicesPlugin.MissingGoogleServicesStrategy
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

// Hosted environments, one product flavor each (see deploy/README.md).
val envs = listOf("staging", "prod")

// Firebase config, never committed: src/<env>/google-services.json from each env's Firebase project,
// else the shared google-services.json next to this file (used by staging and all debug builds).
// Without config a variant still builds, but the app crashes at launch (Firebase is not
// initialized); release builds require it (see verify<Env>ReleaseConfig below).
val googleServicesFiles = envs.associateWith { env ->
    file("src/$env/google-services.json").takeIf { it.exists() } ?: file("google-services.json")
}
if (googleServicesFiles.values.any { it.exists() }) {
    apply(plugin = libs.plugins.google.services.get().pluginId)
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

// Release signing (both envs use the same upload key), never committed: mobile/keystore.properties
// with storeFile (relative to mobile/), storePassword, keyAlias and keyPassword.
val keystorePropertiesFile = rootProject.file("keystore.properties")
val keystoreProperties = Properties().apply {
    if (keystorePropertiesFile.exists()) keystorePropertiesFile.inputStream().use(::load)
}
fun keystoreProperty(key: String): String =
    keystoreProperties.getProperty(key)?.takeIf { it.isNotBlank() }
        ?: throw GradleException("$key is missing from ${keystorePropertiesFile.path} (see deploy/README.md)")

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
    }

    flavorDimensions += "env"
    productFlavors {
        create("staging") {
            dimension = "env"
            versionNameSuffix = "-staging"
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

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release")
        }
    }
}

// CI builds release variants without a signing key: -Pchangeloom.allowUnsignedRelease=true skips
// only the signing check below, and the build output is unsigned.
val allowUnsignedRelease = providers.gradleProperty("changeloom.allowUnsignedRelease").orNull == "true"

// A release build fails early, before compiling, unless its env is fully configured.
androidComponents {
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
            if (!keystorePropertiesFile.exists() && !allowUnsignedRelease) {
                add("${keystorePropertiesFile.path} is missing, so the build would be unsigned")
            }
        }
        val variantName = variant.name.replaceFirstChar { it.uppercase() }
        val verify = tasks.register("verify${variantName}Config") {
            group = "verification"
            description = "Checks that the $env release has an https api URL, Firebase config and a signing key."
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
    implementation(libs.compose.ui.text.google.fonts)
    implementation(libs.compose.material.icons.extended)
    implementation(libs.compose.ui.tooling.preview)
    debugImplementation(libs.compose.ui.tooling)
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
    implementation(libs.androidx.credentials)
    implementation(libs.androidx.credentials.play.services)
    implementation(libs.googleid)
}
