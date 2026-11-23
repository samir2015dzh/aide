package io.aide.android

import io.aide.android.api.ConnectClient
import io.aide.android.api.Device
import io.aide.android.api.LocalApi
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.model.deviceKind
import io.aide.android.model.devicePlatform
import io.aide.android.model.machineKind
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/** 批 J · D, 工单 #100: what a device or machine is drawn as, the same table as packages/core/src/devices.ts. */
class BatchJDTest {
    @Test
    fun devicesByThePlatformTheyLogInWith() {
        val want = mapOf(
            "android" to "android", "android/tablet" to "tablet", "windows" to "windows", "macos" to "mac", "linux" to "linux",
            "ios" to "iphone", "ipados" to "tablet", "darwin/arm64" to "mac", "windows/amd64" to "windows", "linux/amd64" to "linux",
            "freebsd/amd64" to "linux", "" to "device", "plan9/386" to "device", "  Windows " to "windows",
        )
        assertEquals(want, want.mapValues { deviceKind(it.key) })
    }

    @Test
    fun aMachineKeepsItsFaceWhileItDoesNotAnswer() {
        val devices = listOf(Device("k1", "workstation", "windows"))
        assertEquals("linux", machineKind(Machine(machineId = "m", status = MachineStatus(platform = "linux/arm64")), devices))
        assertEquals("windows", machineKind(Machine(machineId = "m", deviceKeyId = "k1"), devices))
        assertEquals("device", machineKind(Machine(machineId = "m"), devices))
    }

    // Every kind has its drawing in the one file both apps draw from (the app's assets get it at build time).
    @Test
    fun everyKindIsDrawnInTheSharedFile() {
        val faces = ConnectClient.json.parseToJsonElement(File("../../../packages/ui/src/faces.json").readText()).jsonObject["faces"]!!.jsonObject
        assertEquals(setOf("person", "android", "iphone", "tablet", "windows", "mac", "linux", "device"), faces.keys)
    }

    @Test
    fun aTabletSaysSo() = runBlocking {
        assertEquals("android", devicePlatform(392))
        assertEquals("android/tablet", devicePlatform(600))
        val server = MockWebServer()
        server.start()
        try {
            server.enqueue(MockResponse().setBody("{}"))
            LocalApi(server.port.toLong(), "tok").logIn("https://relay.example", "owner", "pw", "平板", devicePlatform(800))
            val o = ConnectClient.json.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
            assertEquals("android/tablet", o["platform"]!!.jsonPrimitive.content)
            assertTrue(o["label"]!!.jsonPrimitive.content == "平板")
        } finally {
            server.shutdown()
        }
    }
}
