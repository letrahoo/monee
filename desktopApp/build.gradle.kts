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
    val source = rootProject.layout.projectDirectory.file("assets/brand/app-icon.png")
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
            // JDK jpackage rejects a zero major version even with a separate build number.
            packageVersion = "1.0.1"
            macOS {
                // Build number is separate from the user-visible preview version.
                packageBuildVersion = "1.0.1"
                bundleID = "com.letrahoo.monee"
                iconFile.set(generateMacIcon.map { macIcon.get() })
                infoPlist {
                    extraKeysRawXml = """
                        <key>CFBundleURLTypes</key>
                        <array><dict>
                            <key>CFBundleURLName</key>
                            <string>com.letrahoo.monee.return</string>
                            <key>CFBundleURLSchemes</key>
                            <array><string>monee</string></array>
                        </dict></array>
                    """.trimIndent()
                }
            }
        }
    }
}

// Set the marketing version after jpackage validation, before the DMG consumes the app.
// Re-sign the outer bundle because Info.plist is covered by its ad-hoc signature.
tasks.named("createDistributable") {
    val marketingVersion = providers.gradleProperty("moneeVersion")
    inputs.property("marketingVersion", marketingVersion)
    doLast {
        val app = layout.buildDirectory.dir("compose/binaries/main/app/Monee.app").get().asFile
        providers.exec {
            commandLine("/usr/libexec/PlistBuddy", "-c",
                "Set :CFBundleShortVersionString ${marketingVersion.get()}",
                app.resolve("Contents/Info.plist"))
        }.result.get().assertNormalExitValue()
        providers.exec {
            commandLine("/usr/bin/codesign", "--force", "--sign", "-",
                "--preserve-metadata=entitlements,requirements,flags,runtime", app)
        }.result.get().assertNormalExitValue()
        providers.exec {
            commandLine("/usr/bin/codesign", "--verify", "--deep", "--strict", app)
        }.result.get().assertNormalExitValue()
    }
}
