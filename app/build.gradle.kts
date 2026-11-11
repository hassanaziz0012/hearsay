import java.util.Properties

plugins {
    id("com.android.application")
}

// Release signing comes from keystore.properties (local, not in git) or, on CI, from the
// HEARSAY_KEYSTORE* environment variables. Without either, the release build is unsigned.
val keystoreProps = Properties().apply {
    rootProject.file("keystore.properties").takeIf { it.exists() }?.inputStream()?.use(::load)
}
fun signingValue(prop: String, env: String): String? =
    keystoreProps.getProperty(prop) ?: System.getenv(env)

android {
    namespace = "me.hassandev.hearsay"
    compileSdk {
        version = release(37)
    }

    defaultConfig {
        applicationId = "me.hassandev.hearsay"
        minSdk = 34
        targetSdk = 37
        versionCode = 3
        versionName = "0.4.0"
    }

    signingConfigs {
        create("release") {
            signingValue("storeFile", "HEARSAY_KEYSTORE")?.let { storeFile = file(it) }
            storePassword = signingValue("storePassword", "HEARSAY_KEYSTORE_PASSWORD")
            keyAlias = signingValue("keyAlias", "HEARSAY_KEY_ALIAS")
            keyPassword = signingValue("keyPassword", "HEARSAY_KEY_PASSWORD")
        }
    }

    buildTypes {
        release {
            isDebuggable = false
            signingConfigs.getByName("release").takeIf { it.storeFile != null }
                ?.let { signingConfig = it }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
