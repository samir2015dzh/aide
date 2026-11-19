
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

// The shared Go core (cmd/hermesh-mobile/android), built by gomobile before
// each build as Xcode builds the iPhone's (scripts/build-core.sh skips when
// nothing changed). -PskipCore builds with the library already there.
val coreAar = rootProject.file("build/core/hermeshcore.aar")
val buildCore by tasks.registering(Exec::class) {
    description = "Builds the shared Go core with gomobile"
    commandLine(rootProject.file("scripts/build-core.sh").absolutePath)
    onlyIf { !project.hasProperty("skipCore") }
}

android {
    namespace = "io.hermesh.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.hermesh.android"
        // Android 11: Keystore keys bound to biometrics or the screen lock
        // (setUserAuthenticationParameters), BiometricPrompt with both.
        minSdk = 30
        targetSdk = 35
        versionCode = 1
        versionName = (project.findProperty("appVersion") as String?) ?: "0.0.0-dev"
        // The ABIs the core is built for (build-core.sh): phones and the emulator.
        ndk { abiFilters += listOf("arm64-v8a", "x86_64") }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
        }
        release {
            isMinifyEnabled = false
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
    implementation("androidx.fragment:fragment-ktx:1.8.8")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.2")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.2")
    implementation("androidx.lifecycle:lifecycle-process:2.9.2")
    implementation("androidx.biometric:biometric:1.1.0")

    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")

    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
}
