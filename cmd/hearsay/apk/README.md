`hearsay setup` installs `hearsay.apk` from this folder, embedded in the binary at build
time. The release build puts the signed APK here:

    ./gradlew assembleRelease
    cp app/build/outputs/apk/release/app-release.apk cmd/hearsay/apk/hearsay.apk
    go build -o bin/hearsay ./cmd/hearsay

The APK is gitignored. Without it, setup still works with a phone that already has the
app, or with `hearsay setup --apk FILE`.
