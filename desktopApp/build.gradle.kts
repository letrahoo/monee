import org.jetbrains.compose.desktop.application.dsl.TargetFormat

plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.compose.multiplatform)
    alias(libs.plugins.compose.compiler)
}

kotlin { jvmToolchain(17) }

dependencies {
    implementation(project(":shared"))
    implementation(compose.desktop.currentOs)
    implementation(libs.coroutines.swing)
}

val macIcon = layout.buildDirectory.file("generated/appIcon/Monee.icns")
val generateMacIcon by tasks.registering(Exec::class) {
    val source = rootProject.layout.projectDirectory.file("assets/brand/logo.png")
    val script = rootProject.layout.projectDirectory.file("scripts/generate-macos-icon.sh")
    inputs.files(source, script)
    outputs.file(macIcon)
    commandLine("/bin/bash", script.asFile, source.asFile, macIcon.get().asFile)
}

compose.desktop {
    application {
        mainClass = "com.letrahoo.monee.desktop.MainKt"
        nativeDistributions {
            targetFormats(TargetFormat.Dmg)
            packageName = "Monee"
            // jpackage on macOS requires a non-zero leading component, including for previews.
            packageVersion = "1.0.0"
            macOS {
                bundleID = "com.letrahoo.monee"
                iconFile.set(generateMacIcon.map { macIcon.get() })
            }
        }
    }
}
