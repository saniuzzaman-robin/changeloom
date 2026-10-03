# R8 rules for release builds. kotlinx.serialization, Ktor, Koin, Compose and Firebase ship their own consumer rules;
# this file only adds what they don't cover.

# Readable Crashlytics stack traces (deobfuscated with the uploaded mapping file).
-keepattributes SourceFile,LineNumberTable
-keep public class * extends java.lang.Exception

# kotlinx.serialization: Ktor looks serializers up from the reified type (serializer(KType)), which reaches the
# generated Companion.serializer() reflectively. The library's rules keep these for @Serializable classes; this
# also keeps the classes themselves so full-mode R8 can't merge or strip them.
-keep,includedescriptorclasses @kotlinx.serialization.Serializable class dev.changeloom.** { *; }

# Credential Manager loads its Play Services provider by class name.
-if class androidx.credentials.CredentialManager
-keep class androidx.credentials.playservices.** { *; }
