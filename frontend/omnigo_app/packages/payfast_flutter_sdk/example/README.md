# Payfast Flutter SDK — example app

A single-screen checkout that exercises the whole SDK: enter credentials and
transaction details, tap **Pay now**, and the SDK fetches an access token and
opens the Payfast payment WebView.

Only `lib/` and `pubspec.yaml` are committed. The Android and iOS project
folders are generated locally so the repository stays small.

## Run it

```bash
cd example

# Generates android/ and ios/ for this machine (one time)
flutter create .

flutter pub get
flutter run
```

## Platform setup

`flutter create .` writes a stock project; the SDK needs two adjustments.

### Android

Add the internet permission above the `<application>` tag in
`android/app/src/main/AndroidManifest.xml`:

```xml
<uses-permission android:name="android.permission.INTERNET" />
```

### iOS

`webview_flutter` 4.x requires iOS 12 or newer. In `ios/Podfile`, uncomment and
set the platform line:

```ruby
platform :ios, '12.0'
```

Then:

```bash
cd ios && pod install
```

## Using it

1. Enter the **Merchant ID** and **Secured key** issued by Payfast.
2. Leave **UAT / sandbox** on and use UAT credentials, or turn it off and use
   production credentials. Mismatched pairs are the usual cause of a token
   failure.
3. Adjust the amount, customer details, and redirect URLs.
4. Tap **Pay now**.

The basket ID is generated per attempt, so you can retry without collisions.
The result banner under the button reports the callback the SDK fired —
success, failure, or a token error.

> The default redirect URLs point at `merchant.example`, which does not resolve.
> Replace them with URLs you control (or a request-bin style endpoint) so the
> gateway has somewhere real to redirect after payment.
