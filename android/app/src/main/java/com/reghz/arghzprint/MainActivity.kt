package com.reghz.arghzprint

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.Menu
import android.view.MenuItem
import android.webkit.JavascriptInterface
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts

class MainActivity : ComponentActivity() {
    private lateinit var web: WebView
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private var pendingExport: String? = null

    private val openFile = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        fileCallback?.onReceiveValue(uri?.let { arrayOf(it) })
        fileCallback = null
    }

    private val createFile = registerForActivityResult(ActivityResultContracts.CreateDocument("text/html")) { uri ->
        val content = pendingExport
        pendingExport = null
        if (uri == null || content == null) return@registerForActivityResult
        try {
            contentResolver.openOutputStream(uri)?.use { it.write(content.toByteArray()) }
            Toast.makeText(this, "Template exported", Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            Toast.makeText(this, "Export failed: ${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        PrintService.start(this)

        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.textZoom = 100
            addJavascriptInterface(Bridge(), "AndroidBridge")

            webViewClient = object : WebViewClient() {
                // the Go server may not be listening yet right after the service starts
                override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                    if (request.isForMainFrame) view.postDelayed({ view.loadUrl(PrintService.UI_URL) }, 1000)
                }
            }

            webChromeClient = object : WebChromeClient() {
                override fun onShowFileChooser(
                    view: WebView,
                    callback: ValueCallback<Array<Uri>>,
                    params: FileChooserParams,
                ): Boolean {
                    fileCallback?.onReceiveValue(null)
                    fileCallback = callback
                    // files shared from chat/drive apps often lose their text/html type
                    openFile.launch("*/*")
                    return true
                }
            }
        }
        setContentView(web)
        web.loadUrl(PrintService.UI_URL)

        if (!Setup.isComplete(this)) startActivity(Intent(this, SetupActivity::class.java))
    }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menu.add(0, MENU_SETUP, 0, "Setup checklist")
        menu.add(0, MENU_RELOAD, 1, "Reload")
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem): Boolean = when (item.itemId) {
        MENU_SETUP -> {
            startActivity(Intent(this, SetupActivity::class.java)); true
        }
        MENU_RELOAD -> {
            web.reload(); true
        }
        else -> super.onOptionsItemSelected(item)
    }

    override fun onDestroy() {
        web.destroy()
        super.onDestroy()
    }

    // the WebView can't download blob: URLs, so the UI hands exports over here
    inner class Bridge {
        @JavascriptInterface
        fun saveFile(name: String, content: String) {
            runOnUiThread {
                pendingExport = content
                createFile.launch(name)
            }
        }
    }

    companion object {
        private const val MENU_SETUP = 1
        private const val MENU_RELOAD = 2
    }
}
