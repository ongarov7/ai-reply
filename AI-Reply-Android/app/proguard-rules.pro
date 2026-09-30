# kotlinx.serialization keeps the generated serializers reachable through
# reflection-free lookup, but R8 still needs the companion serializer fields.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**

-keepclassmembers class kz.yerek.aireply.domain.model.** {
    *** Companion;
}
-keepclasseswithmembers class kz.yerek.aireply.domain.model.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class kz.yerek.aireply.domain.model.**$$serializer { *; }

# Credential Manager finds its Google Play services provider by reflection.
-if class androidx.credentials.CredentialManager
-keep class androidx.credentials.playservices.** {
  *;
}

# The IME is instantiated by the system from the manifest entry.
-keep class kz.yerek.aireply.keyboard.ReplyKeyboardService { *; }
