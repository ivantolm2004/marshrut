"""Overlay maintained Kotlin adapters and private release-signing configuration."""
import pathlib, shutil
root = pathlib.Path(__file__).resolve().parents[1]
dest = root / 'ui/android/app/src/main'
shutil.copytree(root / 'platform/android/src/main', dest, dirs_exist_ok=True)
generated = dest / 'kotlin/ru/marshrut/marshrut/MainActivity.kt'
if generated.exists(): generated.unlink()
gradle = root / 'ui/android/app/build.gradle.kts'
text = gradle.read_text(encoding='utf-8-sig')
text = text.replace('ru.marshrut.marshrut', 'ru.marshrut.client').replace('minSdk = flutter.minSdkVersion', 'minSdk = 29')
text = text.replace('signingConfig = signingConfigs.getByName("debug")', 'signingConfig = signingConfigs.getByName("personal")')
if 'create("personal")' not in text:
    signing = '''    signingConfigs {
        create("personal") {
            storeFile = file(System.getenv("MARSHRUT_KEYSTORE") ?: error("Personal keystore required"))
            storePassword = System.getenv("MARSHRUT_KEY_PASSWORD") ?: error("Personal key password required")
            keyAlias = "marshrut"
            keyPassword = System.getenv("MARSHRUT_KEY_PASSWORD")
            storeType = "PKCS12"
        }
    }
    packaging { jniLibs { useLegacyPackaging = true } }
'''
    text = text.replace('    buildTypes {', signing + '    buildTypes {')
    text += '\ndependencies { implementation(files("libs/marshrut.aar")) }\n'
if 'proguardFiles(' not in text:
    text = text.replace('        release {', '        release {\n            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")')
gradle.write_text(text, encoding='utf-8')
(gradle.parent / 'proguard-rules.pro').write_text('-keep class go.** { *; }\n-keep class ru.marshrut.mobile.** { *; }\n-keep class ru.marshrut.libbox.** { *; }\n', encoding='utf-8')
print('Android adapters staged; signing secrets are read from environment only.')
