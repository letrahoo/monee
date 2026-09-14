import org.jetbrains.kotlin.gradle.ExperimentalWasmDsl

plugins {
    alias(libs.plugins.kotlin.multiplatform)
    alias(libs.plugins.compose.multiplatform)
    alias(libs.plugins.compose.compiler)
}

kotlin {
    jvm()
    @OptIn(ExperimentalWasmDsl::class)
    wasmJs { browser() }
    jvmToolchain(17)

    sourceSets {
        commonMain.dependencies {
            implementation(libs.compose.runtime)
            implementation(libs.compose.foundation)
            implementation(libs.compose.material)
            implementation(libs.compose.ui)
            implementation(libs.compose.resources)
        }
        commonTest.dependencies { implementation(kotlin("test")) }
    }
}

val prepareBrandResources by tasks.registering(Sync::class) {
    from(rootProject.layout.projectDirectory.file("assets/brand/app-icon.png")) {
        into("drawable")
        rename { "logo.png" }
    }
    from(rootProject.layout.projectDirectory.file("assets/fonts/noto_sans_sc.ttf")) { into("font") }
    from(rootProject.layout.projectDirectory.file("assets/fonts/OFL.txt")) { into("files/licenses") }
    into(layout.buildDirectory.dir("generated/brandResources"))
}

compose.resources {
    packageOfResClass = "com.letrahoo.monee.resources"
    customDirectory(
        sourceSetName = "commonMain",
        directoryProvider = prepareBrandResources.map { layout.buildDirectory.dir("generated/brandResources").get() },
    )
}
