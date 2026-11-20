
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

// The shared Go core (cmd/aide-mobile/android), built by gomobile before
// each build as Xcode builds the iPhone's (scripts/build-core.sh skips when
// nothing changed). -PskipCore builds with the library already there.
val coreAar = rootProject.file("build/core/aidecore.aar")
val buildCore by tasks.registering(Exec::class) {
    description = "Builds the shared Go core with gomobile"
    commandLine(rootProject.file("scripts/build-core.sh").absolutePath)
    onlyIf { !project.hasProperty("skipCore") }
}

android {
    namespace = "io.aide.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.aide.android"
        // Android 11, as the core is built for (build-core.sh -androidapi 30).
        minSdk = 30
        targetSdk = 35
        versionCode = 1
        versionName = (project.findProperty("appVersion") as String?) ?: "0.0.0-dev"
        // The ABIs the core is built for (build-core.sh): phones and the emulator.
        ndk { abiFilters += listOf("arm64-v8a", "x86_64") }
    }

    // The app's own key, the same for every build (the debug APK and a release
    // alike), so each version installs over the last (docs/android-app.md §6.1).
    // Only the environment names it: the keystore and its passwords are never in
    // the repository. Without AIDE_ANDROID_KEYSTORE (a build on one's own
    // computer) the build is signed with Gradle's debug key, as before.
    val appKeystore = System.getenv("AIDE_ANDROID_KEYSTORE").orEmpty()
    signingConfigs {
        if (appKeystore.isNotEmpty()) {
            create("app") {
                storeFile = file(appKeystore)
                storeType = "pkcs12"
                storePassword = System.getenv("AIDE_ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("AIDE_ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("AIDE_ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            if (appKeystore.isNotEmpty()) signingConfig = signingConfigs.getByName("app")
        }
        release {
            isMinifyEnabled = false
            if (appKeystore.isNotEmpty()) signingConfig = signingConfigs.getByName("app")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging {
        jniLibs {
            // The core is one native library of about 45 MB per ABI: compressed
            // in the APK, extracted when installed.
            useLegacyPackaging = true
        }
    }
    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

kotlin {
    compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) }
}

tasks.named("preBuild") { dependsOn(buildCore) }

dependencies {
    implementation(files(coreAar))

    val composeBom = platform("androidx.compose:compose-bom:2025.08.00")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.ui:ui-tooling-preview")
    debugImplementation("androidx.compose.ui:ui-tooling")

    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.2")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.2")
    implementation("androidx.lifecycle:lifecycle-process:2.9.2")

    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")

    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
}
