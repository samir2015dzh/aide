
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
        // What is published and installed (工单 #55): not debuggable, so the system compiles the app and Compose's
        // own startup and scrolling profiles apply — a debuggable build scrolls far worse on a phone. It keeps the
        // identity the published APKs have had (io.aide.android.debug), so it installs over them, keeping the login;
        // without the app's key (a build on one's own computer) it is signed with Gradle's debug key, as before.
        release {
            isMinifyEnabled = false
            applicationIdSuffix = ".debug"
            signingConfig = signingConfigs.getByName(if (appKeystore.isNotEmpty()) "app" else "debug")
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
        // The page pictures (工单 #54, src/test/.../shots) render with Robolectric: only when asked (-Pshots), as
        // they fetch an Android image of their own and take a while; the unit tests run without them.
        unitTests.isIncludeAndroidResources = true
        unitTests.all {
            if (project.hasProperty("shots")) {
                it.systemProperty("roborazzi.test.record", "true")
                // -ProbolectricDir=<dir>: Robolectric's Android image fetched by hand, where it can't download it itself.
                (project.findProperty("robolectricDir") as String?)?.let { dir ->
                    it.systemProperty("robolectric.offline", "true")
                    it.systemProperty("robolectric.dependency.dir", dir)
                }
                it.filter.includeTestsMatching("io.aide.android.shots.*")
            } else {
                it.exclude("**/shots/**")
            }
        }
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
    testImplementation(composeBom)
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
    // The page pictures (-Pshots).
    testImplementation("org.robolectric:robolectric:4.15.1")
    testImplementation("io.github.takahirom.roborazzi:roborazzi:1.46.1")
    testImplementation("io.github.takahirom.roborazzi:roborazzi-compose:1.46.1")
    testImplementation("androidx.compose.ui:ui-test-junit4")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
