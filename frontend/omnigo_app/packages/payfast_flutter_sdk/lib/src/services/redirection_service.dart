import 'package:flutter/material.dart';
import '../models/get_token_response.dart';
import '../services/payfast_api_service.dart';
import '../widgets/payfast_webview.dart';

typedef PayfastCallback = void Function(String url);
typedef PayfastErrorCallback = void Function(String error);

class RedirectionService {
  final String basketId;
  final String amount;
  final String description;
  final String customerEmail;
  final String customerMobile;
  final String merchantId;
  final String merchantName;
  String token;
  final String securedKey;
  final String successUrl;
  final PayfastCallback? successListener;
  final String failureUrl;
  final PayfastCallback? failureListener;
  final String checkoutUrl;
  final bool isUAT;
  final PayfastErrorCallback? tokenErrorListener;

  RedirectionService._({
    required this.basketId,
    required this.amount,
    required this.description,
    required this.customerEmail,
    required this.customerMobile,
    required this.merchantId,
    required this.merchantName,
    required this.token,
    required this.securedKey,
    required this.successUrl,
    this.successListener,
    required this.failureUrl,
    this.failureListener,
    required this.checkoutUrl,
    this.isUAT = true,
    this.tokenErrorListener,
  });

  /// Fetches an access token from the Payfast API
  Future<GetTokenResponse> fetchToken() async {
    final apiService = PayfastApiService(isUAT: isUAT);
    try {
      return await apiService.getToken(
        merchantId: merchantId,
        securedKey: securedKey,
      );
    } finally {
      apiService.dispose();
    }
  }

  /// Fetches token then opens the payment WebView
  Future<dynamic> fetchTokenAndOpenWebView(BuildContext context) async {
    try {
      final tokenResponse = await fetchToken();
      if (tokenResponse.token != null) {
        token = tokenResponse.token!;
        if (context.mounted) {
          return await openWebView(context);
        }
      } else {
        tokenErrorListener?.call(tokenResponse.message ?? 'Token is null');
      }
    } catch (e) {
      tokenErrorListener?.call(e.toString());
    }
    return null;
  }

  /// Opens the payment WebView directly using the current [token]
  Future<dynamic> openWebView(BuildContext context) {
    return Navigator.push(
      context,
      MaterialPageRoute(
        builder: (context) => PayfastWebView(
          basketId: basketId,
          amount: amount,
          description: description,
          customerEmail: customerEmail,
          customerMobile: customerMobile,
          merchantId: merchantId,
          merchantName: merchantName,
          token: token,
          successUrl: successUrl,
          onSuccess: successListener != null ? () => successListener!('') : null,
          failureUrl: failureUrl,
          onFailure: failureListener != null ? () => failureListener!('') : null,
          checkoutUrl: checkoutUrl,
          isUAT: isUAT,
        ),
      ),
    );
  }

  static RedirectionServiceBuilder newBuilder() {
    return RedirectionServiceBuilder();
  }
}

class RedirectionServiceBuilder {
  String? _basketId;
  String? _amount;
  String? _description;
  String? _customerEmail;
  String? _customerMobile;
  String? _merchantId;
  String? _merchantName;
  String _token = '';
  String? _securedKey;
  String? _successUrl;
  PayfastCallback? _successListener;
  String? _failureUrl;
  PayfastCallback? _failureListener;
  String? _checkoutUrl;
  bool _isUAT = true;
  PayfastErrorCallback? _tokenErrorListener;

  RedirectionServiceBuilder setBasketID(String basketId) {
    _basketId = basketId;
    return this;
  }

  /// Sets the transaction amount, rounded to a whole-unit string.
  ///
  /// Accepts integers (`"1500"`) and decimals (`"1500.40"`); decimals are
  /// rounded half-away-from-zero to the nearest whole unit. Use
  /// [setTxnAmount] to send the amount unchanged.
  ///
  /// Throws [ArgumentError] if [amount] is not numeric.
  RedirectionServiceBuilder setAmount(String amount) {
    final parsed = num.tryParse(amount.trim());
    if (parsed == null) {
      throw ArgumentError.value(amount, 'amount', 'Amount must be numeric');
    }
    _amount = parsed.round().toString();
    return this;
  }

  /// Sets the transaction amount as-is (decimal supported)
  RedirectionServiceBuilder setTxnAmount(String amount) {
    _amount = amount;
    return this;
  }

  RedirectionServiceBuilder setDescription(String description) {
    _description = description;
    return this;
  }

  RedirectionServiceBuilder setCustomerEmail(String email) {
    _customerEmail = email;
    return this;
  }

  RedirectionServiceBuilder setCustomerMobile(String mobile) {
    _customerMobile = mobile;
    return this;
  }

  RedirectionServiceBuilder setMerchantId(String merchantId) {
    _merchantId = merchantId;
    return this;
  }

  RedirectionServiceBuilder setMerchantName(String merchantName) {
    _merchantName = merchantName;
    return this;
  }

  RedirectionServiceBuilder setToken(String token) {
    _token = token;
    return this;
  }

  RedirectionServiceBuilder setSecuredKey(String securedKey) {
    _securedKey = securedKey;
    return this;
  }

  RedirectionServiceBuilder setSuccessURL(String url) {
    _successUrl = url;
    return this;
  }

  RedirectionServiceBuilder setSuccessListener(PayfastCallback listener) {
    _successListener = listener;
    return this;
  }

  RedirectionServiceBuilder setFailureURL(String url) {
    _failureUrl = url;
    return this;
  }

  RedirectionServiceBuilder setFailureListener(PayfastCallback listener) {
    _failureListener = listener;
    return this;
  }

  RedirectionServiceBuilder setCheckoutURL(String url) {
    _checkoutUrl = url;
    return this;
  }

  RedirectionServiceBuilder setIsUAT(bool isUAT) {
    _isUAT = isUAT;
    return this;
  }

  RedirectionServiceBuilder setTokenErrorListener(PayfastErrorCallback listener) {
    _tokenErrorListener = listener;
    return this;
  }

  RedirectionService build() {
    if (_basketId == null) throw Exception('Basket ID is required');
    if (_amount == null) throw Exception('Amount is required');
    if (_description == null) throw Exception('Description is required');
    if (_customerEmail == null) throw Exception('Customer email is required');
    if (_customerMobile == null) throw Exception('Customer mobile is required');
    if (_merchantId == null) throw Exception('Merchant ID is required');
    if (_merchantName == null) throw Exception('Merchant name is required');
    if (_securedKey == null) throw Exception('Secured key is required');
    if (_successUrl == null) throw Exception('Success URL is required');
    if (_failureUrl == null) throw Exception('Failure URL is required');
    if (_checkoutUrl == null) throw Exception('Checkout URL is required');

    return RedirectionService._(
      basketId: _basketId!,
      amount: _amount!,
      description: _description!,
      customerEmail: _customerEmail!,
      customerMobile: _customerMobile!,
      merchantId: _merchantId!,
      merchantName: _merchantName!,
      token: _token,
      securedKey: _securedKey!,
      successUrl: _successUrl!,
      successListener: _successListener,
      failureUrl: _failureUrl!,
      failureListener: _failureListener,
      checkoutUrl: _checkoutUrl!,
      isUAT: _isUAT,
      tokenErrorListener: _tokenErrorListener,
    );
  }
}
