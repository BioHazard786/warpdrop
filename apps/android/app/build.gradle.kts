import java.util.Properties

// Release signing: local.properties (storeFile, storePassword, keyAlias, keyPassword) or, in CI, the same
// names as WARPDROP_* env vars. Without either, release builds are left unsigned.
val signing = Properties().apply { rootProject.file("local.properties").takeIf { it.exists() }?.reader()?.use(::load) }
fun signingValue(key: String): String? = signing.getProperty(key) ?: System.getenv("WARPDROP_" + key.uppercase())

// One version for the CLI, server, Android and desktop: the repository's VERSION file (the release tag is v<VERSION>).
// versionCode is derived (2.1.0 -> 20100), so it only ever rises with the version.
val (major, minor, patch) = rootProject.file("../../VERSION").readText().trim().split(".").map { it.toInt() }

plugins {
  alias(libs.plugins.android.application)
  alias(libs.plugins.compose.compiler)
  alias(libs.plugins.aboutlibraries) // generates R.raw.aboutlibraries (open-source licenses) during the build
}

android {
  namespace = "info.warplabs.warpdrop"
  compileSdk { version = release(37) { minorApiLevel = 2 } }
  defaultConfig {
    applicationId = "com.biohazard786.warpdrop" // namespace stays info.warplabs.warpdrop: it only names the Kotlin packages
    minSdk = 29
    targetSdk = 37
    versionCode = major * 10_000 + minor * 100 + patch
    versionName = "$major.$minor.$patch"
  }

  val releaseSigning = signingValue("storeFile")?.let { path ->
    signingConfigs.create("release") {
      storeFile = file(path) // relative to app/, so ../keystore.jks is apps/android/keystore.jks
      storePassword = signingValue("storePassword")
      keyAlias = signingValue("keyAlias")
      keyPassword = signingValue("keyPassword")
    }
  }

  // one APK per CPU (phones are arm; x86 is emulator-only); no universal APK, each is a fraction of the size
  splits {
    abi {
      isEnable = true
      reset()
      include("arm64-v8a", "armeabi-v7a")
      isUniversalApk = false
    }
  }

  buildTypes {
    release {
      signingConfig = releaseSigning
      isMinifyEnabled = true
      isShrinkResources = true
      proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
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
    resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" }
  }
}

kotlin {
  jvmToolchain(21)
}

aboutLibraries {
  // Go modules inside warpcore.aar aren't Gradle dependencies; `make core` writes their entries here
  collect { configPath = file("aboutlibraries") }
}

dependencies {
  // gomobile output (see Makefile). A local .aar is only allowed in an application module, which is why
  // the Go-backed WarpCore lives here and the shared module only knows the interface.
  implementation(files("libs/warpcore.aar"))

  implementation(platform(libs.androidx.compose.bom))
  implementation(libs.androidx.compose.material3)
  implementation(libs.androidx.core.ktx)
  implementation(libs.androidx.activity.compose)
  implementation(libs.androidx.lifecycle.runtime.compose)
  implementation(libs.androidx.datastore.preferences)
  implementation(libs.androidx.fragment)
  implementation(libs.kotlinx.coroutines.android)
  implementation(libs.qrose)
  implementation(libs.aboutlibraries.compose.m3)
  implementation(libs.play.services.code.scanner)

  testImplementation(libs.kotlin.test)
  testImplementation(libs.kotlinx.coroutines.test)
}
