package com.nw5w.graywolf

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.util.Log

class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Log.i(TAG, "onCreate (skeleton)")
        startForegroundService(Intent(this, GraywolfService::class.java))
    }

    companion object { private const val TAG = "MainActivity" }
}
