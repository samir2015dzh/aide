package io.aide.android

import android.app.Application
import io.aide.android.model.AppModel

/** Starts the owner role (the shared Go core) with the app's process, as the iPhone app does on launch. */
class HermeshApp : Application() {
    lateinit var model: AppModel
        private set

    override fun onCreate() {
        super.onCreate()
        model = AppModel(this)
        model.start()
    }
}
