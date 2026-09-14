import org.jetbrains.kotlin.gradle.ExperimentalWasmDsl

plugins {
    alias(libs.plugins.kotlin.multiplatform)
    alias(libs.plugins.compose.multiplatform)
    alias(libs.plugins.compose.compiler)
}

kotlin {
    @OptIn(ExperimentalWasmDsl::class)
    wasmJs {
        browser {
            commonWebpackConfig { outputFileName = "monee.js" }
        }
        binaries.executable()
    }
    sourceSets {
        commonMain.dependencies {
            implementation(project(":shared"))
            implementation(libs.compose.ui)
        }
    }
}
