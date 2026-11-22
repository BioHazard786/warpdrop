# gomobile's generated Java glue is called from native code by name
-keep class go.** { *; }
-keep class info.warplabs.warpdrop.core.** { *; }
# ML Kit (QR scanner) builds its registrars by reflection from manifest metadata. R8 full mode keeps the classes
# but strips their no-arg constructors, and GmsBarcodeScanning.getClient() then crashes with an NPE.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); }
