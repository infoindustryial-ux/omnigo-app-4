# Payfast Flutter SDK

A Flutter SDK for integrating the Payfast payment gateway with WebView redirection support.

## Features

- Token generation via Payfast API (`MERCHANT_ID` + `SECURED_KEY`)
- Automatic token fetch + WebView launch in one call
- UAT and Production environment switching
- WebView-based payment flow with POST to gateway
- Success and failure callbacks
- Builder pattern for easy configuration

## Requirements

- Flutter: `>=3.10.0`
- Dart: `>=3.0.0`
- Android: `minSdkVersion 21` or higher
- iOS: `12.0` or higher

## Installation

### Option A — Local path (recommended for initial integration)

Copy the `payfast_flutter_sdk` folder into your project, then add to your `pubspec.yaml`:

```yaml
dependencies:
  payfast_flutter_sdk:
    path: ./payfast_flutter_sdk
```

### Option B — Git

The SDK lives in a subdirectory of the redirection SDK repository, so the
dependency needs a `path`:

```yaml
dependencies:
  payfast_flutter_sdk:
    git:
      url: https://bitbucket.org/abdulahad-payfast/redirectionsdk.git
      path: payfast_flutter_sdk
```

Then run:

```bash
flutter pub get
```

---

## Platform setup

### Android

Add the internet permission above the `<application>` tag in
`android/app/src/main/AndroidManifest.xml`:

```xml
<uses-permission android:name="android.permission.INTERNET" />
```

`minSdkVersion` must be 21 or higher (`android/app/build.gradle`):

```groovy
defaultConfig {
    minSdkVersion 21
}
```

The gateway is served over HTTPS, so no cleartext-traffic exception is needed.

### iOS

`webview_flutter` 4.x is backed by `WKWebView` and requires **iOS 12.0 or
newer**. Set the platform in `ios/Podfile`:

```ruby
platform :ios, '12.0'
```

Match it in Xcode under *Runner → General → Minimum Deployments*, or set
`IPHONEOS_DEPLOYMENT_TARGET = 12.0` in `ios/Runner.xcodeproj`. Then:

```bash
cd ios && pod install
```

No `Info.plist` entries are required — the gateway and token API are both
HTTPS, so App Transport Security passes unmodified. Only add an ATS exception
if your own success/failure/checkout URLs are served over plain HTTP.

---

## Example app

A runnable checkout screen lives in [`example/`](example). Platform folders are
generated locally to keep the repository small:

```bash
cd example
flutter create .    # generates android/ and ios/, one time
flutter pub get
flutter run
```

See [`example/README.md`](example/README.md) for details.

---

## Quick Start

The simplest integration — fetch the token and open the WebView in one call:

```dart
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

final service = RedirectionService.newBuilder()
    .setMerchantId('YOUR_MERCHANT_ID')
    .setSecuredKey('YOUR_SECURED_KEY')
    .setBasketID('BASKET-001')
    .setTxnAmount('1500.00')
    .setDescription('Order #001')
    .setCustomerEmail('customer@example.com')
    .setCustomerMobile('03001234567')
    .setMerchantName('My Store')
    .setSuccessURL('https://yourapp.com/success')
    .setFailureURL('https://yourapp.com/failure')
    .setCheckoutURL('https://yourapp.com/checkout')
    .setIsUAT(true)  // false for production
    .setTokenErrorListener((error) {
      print('Token error: $error');
    })
    .setSuccessListener((url) {
      print('Payment successful');
    })
    .setFailureListener((url) {
      print('Payment failed');
    })
    .build();

// Fetches token then opens WebView automatically
await service.fetchTokenAndOpenWebView(context);
```

---

## Step-by-Step Usage

### Step 1 — Fetch token only

If you want to manage the token yourself:

```dart
final service = RedirectionService.newBuilder()
    .setMerchantId('YOUR_MERCHANT_ID')
    .setSecuredKey('YOUR_SECURED_KEY')
    // ... other fields
    .build();

final tokenResponse = await service.fetchToken();

if (tokenResponse.token != null) {
  print('Token: ${tokenResponse.token}');
} else {
  print('Failed: ${tokenResponse.message}');
}
```

### Step 2 — Open WebView with existing token

If you already have a token (e.g. fetched server-side):

```dart
final service = RedirectionService.newBuilder()
    .setMerchantId('YOUR_MERCHANT_ID')
    .setSecuredKey('YOUR_SECURED_KEY')
    .setToken('EXISTING_TOKEN')
    .setBasketID('BASKET-001')
    .setTxnAmount('1500.00')
    // ... other required fields
    .build();

service.openWebView(context);
```

---

## Environments

| Flag | Base URL |
|------|----------|
| `setIsUAT(true)` *(default)* | `https://ipguat.apps.net.pk/Ecommerce/api/` |
| `setIsUAT(false)` | `https://ipg1.apps.net.pk/Ecommerce/api/` |

---

## API Reference

### `RedirectionService.newBuilder()`

Returns a `RedirectionServiceBuilder`. All `set*` methods return the builder for chaining.

| Method | Required | Description |
|--------|----------|-------------|
| `setMerchantId(String)` | Yes | Merchant ID issued by Payfast |
| `setSecuredKey(String)` | Yes | Secured key / shared secret |
| `setBasketID(String)` | Yes | Unique transaction / basket ID |
| `setAmount(String)` | Yes* | Amount — rounded to a whole-unit string (`"1500.60"` → `"1501"`). Throws `ArgumentError` if not numeric. |
| `setTxnAmount(String)` | Yes* | Amount — sent unchanged (supports decimals) |
| `setDescription(String)` | Yes | Payment description shown to customer |
| `setCustomerEmail(String)` | Yes | Customer email address |
| `setCustomerMobile(String)` | Yes | Customer mobile number |
| `setMerchantName(String)` | Yes | Merchant display name |
| `setSuccessURL(String)` | Yes | URL Payfast redirects to on success |
| `setFailureURL(String)` | Yes | URL Payfast redirects to on failure |
| `setCheckoutURL(String)` | Yes | Checkout return URL |
| `setIsUAT(bool)` | No | `true` = UAT, `false` = Production (default: `true`) |
| `setToken(String)` | No | Pre-fetched token (skip `fetchToken` if provided) |
| `setSuccessListener(fn)` | No | Callback when payment succeeds |
| `setFailureListener(fn)` | No | Callback when payment fails |
| `setTokenErrorListener(fn)` | No | Callback when token fetch fails |

*Use either `setAmount` or `setTxnAmount` — not both. They write the same
field, so the last call wins.

`build()` throws an `Exception` naming the first missing required field.

### `RedirectionService` methods

| Method | Description |
|--------|-------------|
| `fetchToken()` | Fetches an access token. Returns `GetTokenResponse`. |
| `fetchTokenAndOpenWebView(BuildContext)` | Fetches token then opens the payment WebView. |
| `openWebView(BuildContext)` | Opens WebView using the current token directly. |

### `PayfastApiService`

Low-level API client. Use `RedirectionService` for most integrations.

```dart
final apiService = PayfastApiService(isUAT: true);

final response = await apiService.getToken(
  merchantId: 'YOUR_MERCHANT_ID',
  securedKey: 'YOUR_SECURED_KEY',
);

apiService.dispose();
```

### `GetTokenResponse`

| Field | Type | Description |
|-------|------|-------------|
| `token` | `String?` | The access token (`ACCESS_TOKEN`) |
| `status` | `String?` | Response status from Payfast |
| `message` | `String?` | Human-readable message (check on failure) |

---

## Complete Example

```dart
import 'package:flutter/material.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

class PaymentScreen extends StatelessWidget {
  const PaymentScreen({Key? key}) : super(key: key);

  Future<void> _pay(BuildContext context) async {
    final service = RedirectionService.newBuilder()
        .setMerchantId('YOUR_MERCHANT_ID')
        .setSecuredKey('YOUR_SECURED_KEY')
        .setBasketID('BASKET-${DateTime.now().millisecondsSinceEpoch}')
        .setTxnAmount('2500.00')
        .setDescription('Order Payment')
        .setCustomerEmail('customer@example.com')
        .setCustomerMobile('03001234567')
        .setMerchantName('My Store')
        .setSuccessURL('https://yourapp.com/success')
        .setFailureURL('https://yourapp.com/failure')
        .setCheckoutURL('https://yourapp.com/checkout')
        .setIsUAT(true)
        .setSuccessListener((url) {
          ScaffoldMessenger.of(context)
              .showSnackBar(const SnackBar(content: Text('Payment successful!')));
        })
        .setFailureListener((url) {
          ScaffoldMessenger.of(context)
              .showSnackBar(const SnackBar(content: Text('Payment failed.')));
        })
        .setTokenErrorListener((error) {
          ScaffoldMessenger.of(context)
              .showSnackBar(SnackBar(content: Text('Error: $error')));
        })
        .build();

    await service.fetchTokenAndOpenWebView(context);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Checkout')),
      body: Center(
        child: ElevatedButton(
          onPressed: () => _pay(context),
          child: const Text('Pay Now'),
        ),
      ),
    );
  }
}
```

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `webview_flutter` | ^4.4.0 | Payment gateway WebView |
| `dio` | ^5.4.3+1 | HTTP client for token API |
| `intl` | ^0.19.0 | Order date formatting |

---

## Running the tests

```bash
flutter pub get
flutter analyze
flutter test
```

The suite covers the token request/response models, the token API client
(against a stubbed HTTP adapter — no network calls), and every builder
validation and amount-formatting rule.

---

## License

Copyright © Payfast. All rights reserved.

## Support

For issues and questions, contact Payfast support.
