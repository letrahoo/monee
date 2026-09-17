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

// Bundle only build outputs. Private OAuth configuration remains in the user's data directory.
val bundledService = layout.buildDirectory.file("generated/service/monee")
val buildBundledService by tasks.registering(Exec::class) {
    inputs.files(rootProject.fileTree("server") { exclude("**/*_test.go") })
    outputs.file(bundledService)
    workingDir(rootProject.projectDir)
    environment("GOPATH", System.getenv("GOPATH") ?: rootProject.file(".local/go").absolutePath)
    environment("GOCACHE", System.getenv("GOCACHE") ?: rootProject.file(".local/go-build").absolutePath)
    doFirst { bundledService.get().asFile.parentFile.mkdirs() }
    commandLine("go", "-C", "server", "build", "-trimpath", "-o", bundledService.get().asFile.absolutePath, "./cmd/monee")
}
val bundleDesktopResources by tasks.registering(Sync::class) {
    dependsOn(buildBundledService, ":webApp:wasmJsBrowserDistribution")
    from(bundledService) { into("common/monee-service"); filePermissions { unix("rwxr-xr-x") } }
    from(project(":webApp").layout.buildDirectory.dir("dist/wasmJs/productionExecutable")) { into("common/monee-service/web") }
    into(layout.buildDirectory.dir("generated/applicationResources"))
}

compose.desktop {
    application {
        mainClass = "com.letrahoo.monee.desktop.MainKt"
        nativeDistributions {
            appResourcesRootDir.set(bundleDesktopResources.map { layout.buildDirectory.dir("generated/applicationResources").get() })
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
tasks.matching { it.name == "createDistributable" }.configureEach {
    val marketingVersion = providers.gradleProperty("moneeVersion")
    inputs.property("marketingVersion", marketingVersion)
    // Compose 1.11's resource preparation dependency alone does not invalidate jpackage.
    // Track resource contents explicitly so Go-only or Web-only changes rebuild the app image.
    dependsOn(bundleDesktopResources)
    inputs.dir(bundleDesktopResources.map { it.destinationDir })
        .withPropertyName("bundledLocalServiceAndWeb")
        .withPathSensitivity(PathSensitivity.RELATIVE)
    doLast {
        val app = layout.buildDirectory.dir("compose/binaries/main/app/Monee.app").get().asFile
        // jpackage copies app resources with mode 0644, even when the staged binary is 0755.
        // Repair before signing the bundle; never mutate permissions in an installed signed app.
        val service = app.resolve("Contents/app/resources/monee-service/monee")
        val webIndex = app.resolve("Contents/app/resources/monee-service/web/index.html")
        check(service.isFile && webIndex.isFile) { "Packaged local service or Web assets are missing" }
        check(service.setExecutable(true, false) && service.canExecute()) { "Cannot make packaged local service executable" }
        providers.exec {
            commandLine("/usr/bin/codesign", "--force", "--sign", "-", service)
        }.result.get().assertNormalExitValue()
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
