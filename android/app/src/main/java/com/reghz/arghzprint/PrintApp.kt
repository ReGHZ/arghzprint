package com.reghz.arghzprint

import android.app.Application
import android.webkit.WebView

class PrintApp : Application() {
    override fun onCreate() {
        super.onCreate()
        // must happen before any WebView exists, otherwise only the visible part of a long receipt gets drawn
        WebView.enableSlowWholeDocumentDraw()
    }
}
