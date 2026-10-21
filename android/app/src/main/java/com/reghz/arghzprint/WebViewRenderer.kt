package com.reghz.arghzprint

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.os.Handler
import android.os.Looper
import android.view.View
import android.webkit.WebView
import android.webkit.WebViewClient
import mobile.HTMLRenderer
import java.io.ByteArrayOutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.math.max
import kotlin.math.roundToInt

// Go calls this from its own thread; the Go side already serializes calls
class WebViewRenderer(private val context: Context) : HTMLRenderer {
    private val main = Handler(Looper.getMainLooper())

    override fun renderPNG(html: String?, cssWidthPx: Long, widthPx: Long): ByteArray {
        val latch = CountDownLatch(1)
        var result: ByteArray? = null
        var failure: Throwable? = null

        // WebView only lives on the main thread — hop over and wait
        main.post {
            try {
                render(html ?: "", cssWidthPx.toInt(), widthPx.toInt()) { bytes, err ->
                    result = bytes
                    failure = err
                    latch.countDown()
                }
            } catch (t: Throwable) {
                failure = t
                latch.countDown()
            }
        }

        if (!latch.await(TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
            throw Exception("webview render timed out after ${TIMEOUT_SECONDS}s")
        }
        failure?.let { throw Exception(it.message ?: it.toString()) }
        return result ?: throw Exception("webview render returned nothing")
    }

    // fresh WebView per receipt — no leftover size or state from the previous job
    private fun render(html: String, cssWidth: Int, targetWidth: Int, done: (ByteArray?, Throwable?) -> Unit) {
        val density = context.resources.displayMetrics.density
        // CSS px are dp on Android, so the physical width is css × density
        val physWidth = max(1, (cssWidth * density).roundToInt())

        val web = WebView(context)
        web.settings.javaScriptEnabled = true
        // the system font-size setting must not change receipt size
        web.settings.textZoom = 100
        web.setBackgroundColor(Color.WHITE)
        web.measure(exactly(physWidth), exactly(1))
        web.layout(0, 0, physWidth, 1)

        var finished = false
        val finish = { bytes: ByteArray?, err: Throwable? ->
            if (!finished) {
                finished = true
                web.destroy()
                done(bytes, err)
            }
        }

        web.webViewClient = object : WebViewClient() {
            override fun onPageFinished(view: WebView, url: String?) {
                waitForContent(web, 0) { cssHeight ->
                    val physHeight = max(1, (cssHeight * density).roundToInt())
                    web.measure(exactly(physWidth), exactly(physHeight))
                    web.layout(0, 0, physWidth, physHeight)
                    // give the relayout a frame before drawing, or the bitmap can come out blank
                    main.postDelayed({
                        try {
                            finish(snapshot(web, physWidth, physHeight, targetWidth), null)
                        } catch (t: Throwable) {
                            finish(null, t)
                        }
                    }, DRAW_DELAY_MS)
                }
            }
        }
        // the Go side gives up at TIMEOUT_SECONDS; without this a stuck page would leak its WebView
        main.postDelayed({ finish(null, Exception("webview render timed out")) }, TIMEOUT_SECONDS * 1000)
        web.loadDataWithBaseURL(null, html, "text/html", "UTF-8", null)
    }

    // images (the feedback QR is a data: URL) can still be decoding at onPageFinished
    private fun waitForContent(web: WebView, tries: Int, ready: (Int) -> Unit) {
        web.evaluateJavascript(
            "(function(){var ok=Array.prototype.every.call(document.images,function(i){return i.complete});" +
                "return ok?document.documentElement.scrollHeight:-1})()"
        ) { value ->
            val h = value?.toIntOrNull() ?: -1
            when {
                h > 0 -> ready(h)
                tries >= MAX_WAIT_TRIES -> ready(max(web.contentHeight, 1))
                else -> main.postDelayed({ waitForContent(web, tries + 1, ready) }, 50)
            }
        }
    }

    private fun snapshot(web: WebView, physWidth: Int, physHeight: Int, targetWidth: Int): ByteArray {
        val full = Bitmap.createBitmap(physWidth, physHeight, Bitmap.Config.ARGB_8888)
        Canvas(full).apply {
            drawColor(Color.WHITE)
            web.draw(this)
        }

        val targetHeight = max(1, (physHeight.toLong() * targetWidth / physWidth).toInt())
        val scaled = Bitmap.createScaledBitmap(full, targetWidth, targetHeight, true)

        val out = ByteArrayOutputStream()
        scaled.compress(Bitmap.CompressFormat.PNG, 100, out)
        if (scaled !== full) scaled.recycle()
        full.recycle()
        return out.toByteArray()
    }

    private fun exactly(size: Int) = View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY)

    companion object {
        private const val TIMEOUT_SECONDS = 15L
        private const val DRAW_DELAY_MS = 150L
        private const val MAX_WAIT_TRIES = 40
    }
}
