package io.aide.android.model

import io.aide.android.api.Device
import io.aide.android.api.Machine

// What a device or machine is, for its face (工单 #100), as packages/core/src/devices.ts says it: by the platform it
// reports, a trusted device's ("android", "android/tablet", "windows", "macos", "linux", "ios") or a machine's
// GOOS/GOARCH ("darwin/arm64"). Linux is drawn as a server; what says nothing known is a device, never a letter.

/** The face's kind: a key of packages/ui/src/faces.json. */
fun deviceKind(platform: String): String {
    val p = platform.trim().lowercase()
    val os = p.substringBefore('/')
    return when {
        "tablet" in p || os == "ipados" || os == "ipad" -> "tablet"
        os == "android" -> "android"
        os == "ios" || os == "iphone" -> "iphone"
        os == "windows" || os == "win32" -> "windows"
        os == "darwin" || os == "macos" || os == "mac" -> "mac"
        os == "linux" || os == "freebsd" || os == "openbsd" || os == "netbsd" -> "linux"
        else -> "device"
    }
}

/** A machine's kind: what it reports while it answers, else what the device it is logged in with logged in as. */
fun machineKind(m: Machine, devices: List<Device>): String =
    deviceKind(m.status?.platform?.ifEmpty { null } ?: devices.firstOrNull { m.deviceKeyId.isNotEmpty() && it.keyId == m.deviceKeyId }?.platform.orEmpty())

/** This device's platform as it logs in: a tablet (the smallest width 600dp and over, Android's own line) says so. */
fun devicePlatform(smallestWidthDp: Int): String = if (smallestWidthDp >= 600) "android/tablet" else "android"
